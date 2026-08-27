// The page reads live state from the Go backend over the documented JSON API.
// It never falls back to built-in sample data.

async function loadHealth() {
  const res = await fetch('/api/health')
  const data = await res.json()
  document.getElementById('service').textContent = data.service ?? '—'
  document.getElementById('status').textContent = data.status ?? '—'
  document.getElementById('error-codes').textContent = data.error_codes ?? '—'
}

async function loadTasks() {
  const list = document.getElementById('task-list')
  try {
    const res = await fetch('/api/tasks')
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      list.innerHTML = `<li>${data.code ?? res.status}: 任务查询尚未接入</li>`
      return
    }
    const tasks = await res.json()
    list.innerHTML = ''
    if (!Array.isArray(tasks) || tasks.length === 0) {
      list.innerHTML = '<li>暂无任务</li>'
      return
    }
    for (const t of tasks) {
      const li = document.createElement('li')
      li.textContent = `${t.id ?? '?'} — ${t.status ?? '?'}`
      list.appendChild(li)
    }
  } catch (err) {
    list.innerHTML = `<li>请求失败：${err.message}</li>`
  }
}

loadHealth()
loadTasks()
