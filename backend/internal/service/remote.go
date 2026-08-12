package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"comfyui-console/internal/config"
)

// RemoteExec SSH 远程执行器：实例启停 / GPU 监控 / 素材上传 / 结果读取
// 均通过 SSH 在算力节点上执行。Host 为空时退化为本地执行（旧版本兼容）。
// 当容器已挂载算力节点共享目录（comfyDir/dataDir 在本机存在）时，文件操作
// 自动跳过 SSH 直读本地（localRoots 命中），仅命令执行仍走 SSH。
type RemoteExec struct {
	cfg        config.RemoteConfig
	client     *ssh.Client
	sftpConn   *sftp.Client
	mu         sync.Mutex
	localRoots []string // 本地已挂载的共享根目录（存在本机磁盘上）
}

func NewRemoteExec(cfg config.RemoteConfig) *RemoteExec {
	return &RemoteExec{cfg: cfg}
}

// SetLocalRoots 注册本地已挂载的共享根目录（容器挂载宿主机目录时本机可见），
// 文件操作命中这些前缀时直接本地读写，跳过 SSH。
func (r *RemoteExec) SetLocalRoots(roots []string) {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			r.localRoots = append(r.localRoots, root)
		}
	}
}

// localPath 判断路径是否命中本地共享根目录（可跳过 SSH 直读）
func (r *RemoteExec) localPath(p string) bool {
	for _, root := range r.localRoots {
		if strings.HasPrefix(p, root) {
			return true
		}
	}
	return false
}

func (r *RemoteExec) Enabled() bool {
	return r.cfg.Enabled()
}

// Host 返回算力节点地址；未启用远程时为 127.0.0.1
func (r *RemoteExec) Host() string {
	if !r.Enabled() {
		return "127.0.0.1"
	}
	return r.cfg.Host
}

// dialLocked 建立 SSH 连接（含 sftp），失败时返回错误。调用方必须已持有 r.mu，
// 保证「连接建立 → 使用」原子化，避免并发 reset/Close 导致 sftpConn 失效（nil panic）。
func (r *RemoteExec) dialLocked() error {
	if r.client != nil && r.sftpConn != nil {
		return nil
	}
	port := r.cfg.Port
	if port == 0 {
		port = 22
	}
	auths := []ssh.AuthMethod{}
	if r.cfg.Password != "" {
		auths = append(auths, ssh.Password(r.cfg.Password))
	}
	if keyAuth, err := loadPrivateKeyAuth(r.cfg.PrivateKey, r.cfg.KeyPassphrase); err == nil {
		auths = append(auths, keyAuth)
	}
	cfg := &ssh.ClientConfig{
		User:            r.cfg.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", r.cfg.Host, port), cfg)
	if err != nil {
		return err
	}
	sftpConn, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return err
	}
	r.client = client
	r.sftpConn = sftpConn
	return nil
}

// loadPrivateKeyAuth 从文件加载私钥生成 SSH 公钥认证；path 为空时返回错误
func loadPrivateKeyAuth(path, passphrase string) (ssh.AuthMethod, error) {
	if path == "" {
		return nil, fmt.Errorf("no private key path")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(key)
	}
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

// Close 关闭连接
func (r *RemoteExec) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftpConn != nil {
		_ = r.sftpConn.Close()
		r.sftpConn = nil
	}
	if r.client != nil {
		_ = r.client.Close()
		r.client = nil
	}
}

// reset 断开连接（执行失败时用于触发重连）
func (r *RemoteExec) reset() {
	r.Close()
}

// Run 在远程执行命令，返回 stdout。失败返回含 stderr 的错误。
func (r *RemoteExec) Run(cmd string) (string, error) {
	return r.RunTimeout(cmd, 0)
}

// RunTimeout 执行命令并等待完成；timeout<=0 表示不限制
func (r *RemoteExec) RunTimeout(cmd string, timeout time.Duration) (string, error) {
	if !r.Enabled() {
		return runLocal(cmd, timeout)
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		r.mu.Lock()
		if err = r.dialLocked(); err == nil {
			var out string
			out, err = r.runSSH(cmd, timeout)
			r.mu.Unlock()
			if err == nil {
				return out, nil
			}
			// 连接层错误则重连重试一次
			var exitErr *ssh.ExitError
			if errors.As(err, &exitErr) {
				return out, err
			}
			r.reset()
			continue
		}
		r.mu.Unlock()
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("ssh run failed: %w", err)
}

func (r *RemoteExec) runSSH(cmd string, timeout time.Duration) (string, error) {
	sess, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	done := make(chan error, 1)
	go func() {
		done <- sess.Run(cmd)
	}()
	if timeout > 0 {
		select {
		case err = <-done:
		case <-time.After(timeout):
			_ = sess.Close()
			return "", fmt.Errorf("ssh command timeout: %s", cmd)
		}
	} else {
		err = <-done
	}
	out := stdout.String()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s: %s", cmd, msg)
	}
	return out, nil
}

// StartDaemon 远程后台启动进程（cmd 已含 nohup ... & 形式），不等待子进程结束
func (r *RemoteExec) StartDaemon(cmd string) error {
	if !r.Enabled() {
		return runLocalDaemon(cmd)
	}
	_, err := r.RunTimeout(cmd, 30*time.Second)
	return err
}

// Stat 判断路径存在性（本地挂载命中时直读本地，跳过 SSH）
func (r *RemoteExec) Stat(p string) (os.FileInfo, error) {
	if !r.Enabled() || r.localPath(p) {
		return os.Stat(p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return nil, err
	}
	return r.sftpConn.Stat(p)
}

// ListDir 列出目录内容（本地挂载命中时直读本地，跳过 SSH）
func (r *RemoteExec) ListDir(p string) ([]os.FileInfo, error) {
	if !r.Enabled() || r.localPath(p) {
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			infos = append(infos, info)
		}
		return infos, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return nil, err
	}
	return r.sftpConn.ReadDir(p)
}

// Exists 判断路径是否存在
func (r *RemoteExec) Exists(p string) bool {
	_, err := r.Stat(p)
	return err == nil
}

// MkdirAll 递归创建目录（本地挂载命中时直建本地目录，跳过 SSH）
func (r *RemoteExec) MkdirAll(p string, perm os.FileMode) error {
	if !r.Enabled() || r.localPath(p) {
		return os.MkdirAll(p, perm)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return err
	}
	cur := "/"
	for _, part := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if part == "" {
			continue
		}
		cur = path.Join(cur, part)
		fi, err := r.sftpConn.Stat(cur)
		if err == nil {
			if !fi.IsDir() {
				return fmt.Errorf("%s exists and is not a directory", cur)
			}
			continue
		}
		if err := r.sftpConn.Mkdir(cur); err != nil {
			return err
		}
	}
	return nil
}

// WriteFile 写文件（自动建目录；本地挂载命中时直写本地，跳过 SSH）
func (r *RemoteExec) WriteFile(p string, data []byte, perm os.FileMode) error {
	if !r.Enabled() || r.localPath(p) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, data, perm)
	}
	if err := r.MkdirAll(path.Dir(p), 0o755); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return err
	}
	f, err := r.sftpConn.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Chmod(perm)
}

// Open 打开文件用于读取（本地挂载命中时直读本地，跳过 SSH）
func (r *RemoteExec) Open(p string) (io.ReadCloser, error) {
	if !r.Enabled() || r.localPath(p) {
		return os.Open(p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return nil, err
	}
	return r.sftpConn.Open(p)
}

// OpenSeek 打开文件用于随机读取（支持 Range 请求；本地挂载命中时直读本地）
func (r *RemoteExec) OpenSeek(p string) (io.ReadSeekCloser, error) {
	if !r.Enabled() || r.localPath(p) {
		return os.Open(p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return nil, err
	}
	f, err := r.sftpConn.Open(p)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Size 返回远程文件大小
func (r *RemoteExec) Size(p string) (int64, error) {
	fi, err := r.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// OpenWrite 打开文件用于追加写入（实例日志；本地挂载命中时直写本地）
func (r *RemoteExec) OpenWrite(p string) (io.WriteCloser, error) {
	if !r.Enabled() || r.localPath(p) {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	if err := r.MkdirAll(path.Dir(p), 0o755); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dialLocked(); err != nil {
		return nil, err
	}
	return r.sftpConn.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY)
}

// pythonBin 定位 conda 环境 python
func (r *RemoteExec) PythonBin() string {
	if !r.Enabled() {
		return localPythonBin()
	}
	candidates := []string{
		"/opt/miniconda3/envs/comfyenv/bin/python",
		"/opt/miniconda3/envs/comfyenv/bin/python",
		"/data/miniconda3/envs/comfyenv/bin/python",
		"/opt/conda/envs/comfyenv/bin/python",
	}
	for _, p := range candidates {
		if r.Exists(p) {
			return p
		}
	}
	return "python"
}

func localPythonBin() string {
	candidates := []string{
		"/opt/miniconda3/envs/comfyenv/bin/python",
		"/opt/miniconda3/envs/comfyenv/bin/python",
		"/data/miniconda3/envs/comfyenv/bin/python",
		"/opt/conda/envs/comfyenv/bin/python",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "python"
}

// ---------- 本地执行（remote 未配置时退化） ----------

func runLocal(cmd string, timeout time.Duration) (string, error) {
	c := exec.Command("bash", "-lc", cmd)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if timeout > 0 {
		timer := time.AfterFunc(timeout, func() {
			_ = c.Process.Kill()
		})
		defer timer.Stop()
	}
	err := c.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s: %s", cmd, msg)
	}
	return stdout.String(), nil
}

func runLocalDaemon(cmd string) error {
	c := exec.Command("bash", "-lc", cmd)
	if err := c.Start(); err != nil {
		return err
	}
	go func() {
		_ = c.Wait()
	}()
	return nil
}

var _ = log.Printf
