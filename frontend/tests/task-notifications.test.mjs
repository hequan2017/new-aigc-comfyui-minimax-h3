import test from 'node:test'
import assert from 'node:assert/strict'
import { hasVideoResult, taskSuccessNotification } from '../src/utils/taskNotifications.js'

test('识别 ComfyUI 标记为 images 的 MP4 输出', () => {
  const files = JSON.stringify([
    { type: 'images', filename: 'task_00001_.mp4', subfolder: 'minimax' }
  ])
  assert.equal(hasVideoResult(files), true)
  assert.match(taskSuccessNotification({ task_id: 'task-1', status: 'success', result_files: files }), /视频生成成功/)
})

test('非视频结果和非成功状态不弹成功提醒', () => {
  const image = JSON.stringify([{ type: 'images', filename: 'result.png' }])
  assert.equal(hasVideoResult(image), false)
  assert.equal(taskSuccessNotification({ task_id: 'task-2', status: 'success', result_files: image }), null)
  assert.equal(taskSuccessNotification({ task_id: 'task-3', status: 'failed', result_files: '[]' }), null)
})
