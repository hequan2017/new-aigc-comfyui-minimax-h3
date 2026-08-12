const videoExtension = /\.(mp4|webm|mov|mkv)$/i

export function hasVideoResult(resultFiles) {
  let files = resultFiles
  if (typeof files === 'string') {
    try {
      files = JSON.parse(files)
    } catch (_) {
      return false
    }
  }
  if (!Array.isArray(files)) return false
  return files.some((file) => file?.type === 'videos' || videoExtension.test(file?.filename || ''))
}

export function taskSuccessNotification(event) {
  if (event?.status !== 'success' || !event.task_id || !hasVideoResult(event.result_files)) {
    return null
  }
  return `视频生成成功：任务 ${event.task_id} 已完成，请前往任务中心查看。`
}
