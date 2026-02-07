// Raccoon Recon frontend

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
            <div class="glow-card"></div>
            <h3>${esc(p.name)}</h3>
            <p style="color: var(--text-secondary); margin-bottom: 8px;">${esc(p.description || '')}</p>
            <p style="font-family: var(--font-mono); font-size: 12px; color: var(--text-muted);">${esc(p.scope || 'No scope defined')}</p>
            <div style="margin-top: 12px;">
                <button class="btn btn-sm btn-danger" onclick="deleteProject(${p.id})">Delete</button>
            </div>
        </div>
    `).join('');
    initGlowCards();
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

    let finished = false;
    const markDone = (status) => {
        if (finished) return;
        finished = true;
        statusBadge.textContent = status === 'completed' ? 'Completed' : 'Failed';
        statusBadge.className = status === 'completed' ? 'badge badge-completed' : 'badge badge-failed';
        loadScanResults(scan.id);
    };

    // Connect WebSocket for live output
    const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${wsProto}//${location.host}/ws`);

    ws.onopen = () => {
        ws.send(JSON.stringify({ scan_id: scan.id }));
    };

    ws.onmessage = (evt) => {
        const msg = JSON.parse(evt.data);
        if (msg.done) {
            ws.close();
            markDone('completed');
            return;
        }
        if (!finished) {
            const cls = msg.stream === 'stderr' ? 'line-stderr' : 'line-stdout';
            terminal.innerHTML += `<span class="${cls}">${esc(msg.line)}</span>\n`;
            terminal.scrollTop = terminal.scrollHeight;
        }
    };

    ws.onerror = () => { /* polling handles it */ };

    // Poll as reliable fallback
    pollScanStatus(scan.id, statusBadge, terminal, () => finished, (s) => { markDone(s); try { ws.close(); } catch(e) {} });
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
        metadata: 'completed',
    };
    return map[type] || 'pending';
}

// --- Dashboard Quick Scan ---
function quickScan(tool, inputId, scanType) {
    const input = document.getElementById(inputId);
    const target = input.value.trim();
    if (!target) {
        input.focus();
        return;
    }

    const modal = document.getElementById('qa-modal');
    const terminal = document.getElementById('qa-terminal');
    const statusBadge = document.getElementById('qa-scan-status');
    const resultsDiv = document.getElementById('qa-results');
    const resultsBody = document.getElementById('qa-results-body');
    const title = document.getElementById('qa-modal-title');

    modal.classList.remove('hidden');
    terminal.innerHTML = '';
    statusBadge.textContent = 'Running';
    statusBadge.className = 'badge badge-running';
    resultsDiv.style.display = 'none';
    resultsBody.innerHTML = '';
    title.textContent = tool.replace(/_/g, ' ').toUpperCase() + ' \u2014 ' + target;

    const body = {
        target: target,
        tool: tool,
        scan_type: scanType,
        project_id: 0,
        parameters: '{}',
    };

    fetch('/api/scans', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    }).then(resp => {
        if (!resp.ok) {
            return resp.json().then(err => {
                terminal.innerHTML = `<span class="line-stderr">Error: ${esc(err.error || 'Unknown error')}</span>\n`;
                statusBadge.textContent = 'Failed';
                statusBadge.className = 'badge badge-failed';
                throw new Error('scan failed');
            });
        }
        return resp.json();
    }).then(scan => {
        // Always poll as primary mechanism; WS enhances with live output
        let finished = false;
        const markDone = (status) => {
            if (finished) return;
            finished = true;
            statusBadge.textContent = status === 'completed' ? 'Completed' : 'Failed';
            statusBadge.className = status === 'completed' ? 'badge badge-completed' : 'badge badge-failed';
            loadQAResults(scan.id);
            if (typeof initDashboard === 'function') initDashboard();
        };

        // Try WebSocket for live streaming
        const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(`${wsProto}//${location.host}/ws`);

        ws.onopen = () => {
            ws.send(JSON.stringify({ scan_id: scan.id }));
        };

        ws.onmessage = (evt) => {
            const msg = JSON.parse(evt.data);
            if (msg.done) {
                ws.close();
                markDone('completed');
                return;
            }
            if (!finished) {
                const cls = msg.stream === 'stderr' ? 'line-stderr' : 'line-stdout';
                terminal.innerHTML += `<span class="${cls}">${esc(msg.line)}</span>\n`;
                terminal.scrollTop = terminal.scrollHeight;
            }
        };

        ws.onerror = () => { /* polling handles it */ };

        // Poll scan status as reliable fallback
        pollScanStatus(scan.id, statusBadge, terminal, () => finished, (s) => { markDone(s); try { ws.close(); } catch(e) {} });
    }).catch(() => {});
}

async function pollScanStatus(scanId, statusBadge, terminal, isFinished, onDone) {
    for (let i = 0; i < 60; i++) {
        await new Promise(r => setTimeout(r, 500));
        if (isFinished && isFinished()) return;
        try {
            const resp = await fetch(`/api/scans/${scanId}`);
            if (!resp.ok) continue;
            const scan = await resp.json();
            if (scan.status === 'completed' || scan.status === 'failed') {
                if (scan.raw_output && terminal.innerHTML.trim() === '') {
                    terminal.innerHTML = scan.raw_output.split('\n').map(l =>
                        `<span class="line-stdout">${esc(l)}</span>\n`
                    ).join('');
                }
                if (onDone) { onDone(scan.status); }
                else {
                    statusBadge.textContent = scan.status === 'completed' ? 'Completed' : 'Failed';
                    statusBadge.className = scan.status === 'completed' ? 'badge badge-completed' : 'badge badge-failed';
                    loadQAResults(scanId);
                    if (typeof initDashboard === 'function') initDashboard();
                }
                return;
            }
        } catch (e) { /* continue polling */ }
    }
    if (isFinished && isFinished()) return;
    statusBadge.textContent = 'Timeout';
    statusBadge.className = 'badge badge-failed';
}

async function loadQAResults(scanId) {
    const resp = await fetch(`/api/scans/${scanId}/results`);
    if (!resp.ok) return;

    const results = await resp.json();
    if (!results || results.length === 0) return;

    const resultsDiv = document.getElementById('qa-results');
    const tbody = document.getElementById('qa-results-body');
    resultsDiv.style.display = 'block';

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

function closeQAModal() {
    document.getElementById('qa-modal').classList.add('hidden');
    const preview = document.getElementById('qa-image-preview');
    if (preview) {
        preview.style.display = 'none';
        preview.src = '';
    }
}

// --- File Metadata Upload ---
async function uploadFileMetadata(file) {
    if (!file) return;

    const modal = document.getElementById('qa-modal');
    const terminal = document.getElementById('qa-terminal');
    const statusBadge = document.getElementById('qa-scan-status');
    const resultsDiv = document.getElementById('qa-results');
    const resultsBody = document.getElementById('qa-results-body');
    const title = document.getElementById('qa-modal-title');
    const preview = document.getElementById('qa-image-preview');

    modal.classList.remove('hidden');
    terminal.innerHTML = `<span class="line-stdout">Uploading ${esc(file.name)} (${formatSize(file.size)})...</span>\n`;
    statusBadge.textContent = 'Extracting';
    statusBadge.className = 'badge badge-running';
    resultsDiv.style.display = 'none';
    resultsBody.innerHTML = '';
    title.textContent = 'FILE METADATA \u2014 ' + file.name;

    // Show image preview for image files
    if (preview) {
        if (file.type.startsWith('image/')) {
            const reader = new FileReader();
            reader.onload = (e) => {
                preview.src = e.target.result;
                preview.style.display = 'block';
            };
            reader.readAsDataURL(file);
        } else {
            preview.style.display = 'none';
            preview.src = '';
        }
    }

    // Upload file
    const formData = new FormData();
    formData.append('file', file);

    try {
        const resp = await fetch('/api/upload/metadata', {
            method: 'POST',
            body: formData,
        });

        if (!resp.ok) {
            const err = await resp.json();
            terminal.innerHTML += `<span class="line-stderr">Error: ${esc(err.error || 'Upload failed')}</span>\n`;
            statusBadge.textContent = 'Failed';
            statusBadge.className = 'badge badge-failed';
            return;
        }

        const data = await resp.json();
        terminal.innerHTML += `<span class="line-stdout">Extracted ${data.results.length} metadata fields</span>\n`;
        terminal.innerHTML += `<span class="line-stdout">MIME type: ${esc(data.mime_type)}</span>\n`;
        statusBadge.textContent = 'Completed';
        statusBadge.className = 'badge badge-completed';

        // Display results
        if (data.results && data.results.length > 0) {
            resultsDiv.style.display = 'block';
            resultsBody.innerHTML = data.results.map(r => {
                let displayValue = esc(r.value);
                // Make GPS coordinates a link to map
                if (r.key === 'gps_coordinates') {
                    const coords = r.value;
                    displayValue = `<a href="https://www.google.com/maps?q=${encodeURIComponent(coords)}" target="_blank" rel="noopener" style="color: var(--accent);">${esc(coords)}</a>`;
                }
                return `<tr>
                    <td><span class="badge badge-completed">metadata</span></td>
                    <td style="font-family: var(--font-mono);">${esc(r.key)}</td>
                    <td>${displayValue}</td>
                </tr>`;
            }).join('');
        }
    } catch (e) {
        terminal.innerHTML += `<span class="line-stderr">Error: ${esc(e.message)}</span>\n`;
        statusBadge.textContent = 'Failed';
        statusBadge.className = 'badge badge-failed';
    }
}

function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' bytes';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
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

// --- Glowing card effect ---
let _glowHandler = null;

function initGlowCards() {
    // Remove previous listener if re-initializing (e.g. after dynamic content load)
    if (_glowHandler) {
        document.body.removeEventListener('pointermove', _glowHandler);
    }

    const glowEls = document.querySelectorAll('.glow-card');
    if (glowEls.length === 0) return;

    let mouseX = 0, mouseY = 0;
    let rafId = null;

    function updateGlow() {
        let closestGlow = null;
        let closestDist = Infinity;

        glowEls.forEach(glow => {
            const card = glow.parentElement;
            const rect = card.getBoundingClientRect();

            const isInside = mouseX >= rect.left && mouseX <= rect.right &&
                             mouseY >= rect.top && mouseY <= rect.bottom;

            glow.style.setProperty('--glow-active', '0');
            card.classList.remove('glow-active');

            if (isInside) {
                const cx = rect.left + rect.width / 2;
                const cy = rect.top + rect.height / 2;
                const dist = Math.hypot(mouseX - cx, mouseY - cy);
                if (dist < closestDist) {
                    closestDist = dist;
                    closestGlow = glow;
                }
            }
        });

        if (closestGlow) {
            const card = closestGlow.parentElement;
            const rect = card.getBoundingClientRect();

            // Clamp cursor to card edge to get the nearest border point
            const clampX = Math.max(rect.left, Math.min(mouseX, rect.right));
            const clampY = Math.max(rect.top, Math.min(mouseY, rect.bottom));

            // Angle from card center toward the clamped cursor position
            const cx = rect.left + rect.width / 2;
            const cy = rect.top + rect.height / 2;
            const angle = Math.atan2(clampY - cy, clampX - cx) * (180 / Math.PI) + 90;

            closestGlow.style.setProperty('--glow-active', '1');
            closestGlow.style.setProperty('--glow-start', String(angle));
            card.classList.add('glow-active');
        }

        rafId = null;
    }

    _glowHandler = (e) => {
        mouseX = e.clientX;
        mouseY = e.clientY;
        if (!rafId) {
            rafId = requestAnimationFrame(updateGlow);
        }
    };

    document.body.addEventListener('pointermove', _glowHandler, { passive: true });
}

// --- Init ---
document.addEventListener('DOMContentLoaded', () => {
    loadProjects();
    loadProjectDropdown();
    loadDashboard();
    initGlowCards();
});
