// ReconSuite frontend

// --- Modal helpers ---
function showNewProjectModal() {
    document.getElementById('modal-title').textContent = 'New Project';
    document.getElementById('project-id').value = '';
    document.getElementById('project-form').reset();
    document.getElementById('project-modal').classList.remove('hidden');
}

function closeModal() {
    const modal = document.getElementById('project-modal');
    if (modal) modal.classList.add('hidden');
}

// --- Project CRUD ---
async function saveProject(e) {
    e.preventDefault();
    const id = document.getElementById('project-id').value;
    const data = {
        name: document.getElementById('project-name').value,
        description: document.getElementById('project-desc').value,
        scope: document.getElementById('project-scope').value,
    };

    const method = id ? 'PUT' : 'POST';
    const url = id ? `/api/projects/${id}` : '/api/projects';

    const resp = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });

    if (resp.ok) {
        closeModal();
        location.reload();
    }
}

async function loadProjects() {
    const list = document.getElementById('projects-list');
    if (!list) return;

    const resp = await fetch('/api/projects');
    if (!resp.ok) return;

    const projects = await resp.json();
    if (!projects || projects.length === 0) return;

    list.innerHTML = projects.map(p => `
        <div class="card">
            <h3>${esc(p.name)}</h3>
            <p style="color: var(--text-secondary); margin-bottom: 8px;">${esc(p.description || '')}</p>
            <p style="font-family: var(--font-mono); font-size: 12px; color: var(--text-muted);">${esc(p.scope || 'No scope defined')}</p>
            <div style="margin-top: 12px;">
                <button class="btn btn-sm btn-danger" onclick="deleteProject(${p.id})">Delete</button>
            </div>
        </div>
    `).join('');
}

async function deleteProject(id) {
    if (!confirm('Delete this project and all its data?')) return;
    const resp = await fetch(`/api/projects/${id}`, { method: 'DELETE' });
    if (resp.ok) location.reload();
}

// --- Project dropdown for scan forms ---
async function loadProjectDropdown() {
    const sel = document.getElementById('project_id');
    if (!sel) return;

    const resp = await fetch('/api/projects');
    if (!resp.ok) return;
    const projects = await resp.json();

    sel.innerHTML = '<option value="0">-- No project --</option>';
    if (projects) {
        projects.forEach(p => {
            sel.innerHTML += `<option value="${p.id}">${esc(p.name)}</option>`;
        });
    }
}

// --- Tool options ---
function updateToolOptions() {
    const optDiv = document.getElementById('tool-options');
    if (!optDiv) return;
    const tool = document.getElementById('tool').value;
    optDiv.innerHTML = (typeof toolOptionsConfig !== 'undefined' && toolOptionsConfig[tool]) || '';
}

// --- Scan execution ---
async function runScan(e, scanType) {
    e.preventDefault();

    const target = document.getElementById('target').value.trim();
    const toolSelect = document.getElementById('tool');
    const tool = toolSelect.value;
    const projectId = parseInt(document.getElementById('project_id').value) || 0;

    // Gather tool-specific parameters
    const params = {};
    const selectedOption = toolSelect.options[toolSelect.selectedIndex];
    if (selectedOption.dataset.scanType) {
        params.scan_type = selectedOption.dataset.scanType;
    }

    // Collect all inputs/selects in tool-options
    const optDiv = document.getElementById('tool-options');
    if (optDiv) {
        optDiv.querySelectorAll('input, select').forEach(el => {
            if (el.id && el.value) params[el.id] = el.value;
        });
    }

    const body = {
        target,
        tool,
        scan_type: scanType,
        project_id: projectId,
        parameters: JSON.stringify(params),
    };

    // Show output area
    const outputCard = document.getElementById('scan-output');
    const terminal = document.getElementById('terminal');
    const statusBadge = document.getElementById('scan-status');
    const resultsCard = document.getElementById('scan-results');

    outputCard.style.display = 'block';
    terminal.innerHTML = '';
    statusBadge.textContent = 'Running';
    statusBadge.className = 'badge badge-running';
    resultsCard.style.display = 'none';

    // Start scan
    const resp = await fetch('/api/scans', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });

    if (!resp.ok) {
        const err = await resp.json();
        terminal.innerHTML = `<span class="line-stderr">Error: ${esc(err.error || 'Unknown error')}</span>\n`;
        statusBadge.textContent = 'Failed';
        statusBadge.className = 'badge badge-failed';
        return;
    }

    const scan = await resp.json();

    // Connect WebSocket for live output
    const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${wsProto}//${location.host}/ws`);

    ws.onopen = () => {
        ws.send(JSON.stringify({ scan_id: scan.id }));
    };

    ws.onmessage = (evt) => {
        const msg = JSON.parse(evt.data);
        if (msg.done) {
            statusBadge.textContent = 'Completed';
            statusBadge.className = 'badge badge-completed';
            ws.close();
            loadScanResults(scan.id);
            return;
        }
        const cls = msg.stream === 'stderr' ? 'line-stderr' : 'line-stdout';
        terminal.innerHTML += `<span class="${cls}">${esc(msg.line)}</span>\n`;
        terminal.scrollTop = terminal.scrollHeight;
    };

    ws.onerror = () => {
        statusBadge.textContent = 'Error';
        statusBadge.className = 'badge badge-failed';
    };
}

async function loadScanResults(scanId) {
    const resp = await fetch(`/api/scans/${scanId}/results`);
    if (!resp.ok) return;

    const results = await resp.json();
    if (!results || results.length === 0) return;

    const resultsCard = document.getElementById('scan-results');
    const tbody = document.getElementById('results-body');
    resultsCard.style.display = 'block';

    tbody.innerHTML = results.map(r => {
        let displayValue = r.value;
        if (displayValue.startsWith('http://') || displayValue.startsWith('https://')) {
            displayValue = `<a href="${esc(r.value)}" target="_blank" rel="noopener" style="color: var(--accent);">${esc(r.value)}</a>`;
        } else {
            displayValue = esc(displayValue);
        }
        return `<tr>
            <td><span class="badge badge-${badgeClass(r.result_type)}">${esc(r.result_type)}</span></td>
            <td style="font-family: var(--font-mono);">${esc(r.key)}</td>
            <td>${displayValue}</td>
        </tr>`;
    }).join('');
}

function badgeClass(type) {
    const map = {
        port: 'running', dns: 'completed', whois: 'completed',
        header: 'pending', ssl: 'completed', os: 'running',
        google_dork: 'pending', osint_link: 'pending', raw: 'pending',
    };
    return map[type] || 'pending';
}

// --- Dashboard ---
async function loadDashboard() {
    const statEls = document.querySelectorAll('.stat');
    if (statEls.length < 3) return;

    const resp = await fetch('/api/stats');
    if (!resp.ok) return;
    const stats = await resp.json();

    statEls[0].textContent = stats.project_count;
    statEls[1].textContent = stats.scan_count;
    statEls[2].textContent = stats.result_count;
}

// --- Helpers ---
function esc(text) {
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

// --- Init ---
document.addEventListener('DOMContentLoaded', () => {
    loadProjects();
    loadProjectDropdown();
    loadDashboard();
});
