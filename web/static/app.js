// State
let selectedProcess = null;
let logs = [];
let ws = null;
let searchQuery = '';
let autoScroll = true;
let intentionalClose = false;
let reconnectTimeout = null;

// ANSI to HTML converter
const ansiUp = new AnsiUp();
ansiUp.use_classes = true;

// DOM Elements
const connectionStatus = document.getElementById('connection-status');
const processList = document.getElementById('process-list');
const logTitle = document.getElementById('log-title');
const logContent = document.getElementById('log-content');
const logStats = document.getElementById('log-stats');
const searchStats = document.getElementById('search-stats');
const searchInput = document.getElementById('search-input');
const btnStart = document.getElementById('btn-start');
const btnStop = document.getElementById('btn-stop');
const btnRestart = document.getElementById('btn-restart');
const btnClear = document.getElementById('btn-clear');
const btnDownload = document.getElementById('btn-download');
const logViewer = document.getElementById('log-viewer');

// Initialize
async function init() {
    const processes = await fetchProcesses();

    // Connect once to receive all logs - filtering happens client-side
    connectWebSocket();
    setupEventListeners();

    // Select first process by default (this does NOT reconnect WebSocket)
    if (processes.length > 0) {
        selectProcess(processes[0].name);
    }

    // Refresh process list periodically
    setInterval(fetchProcesses, 5000);
}

// Fetch processes from API
async function fetchProcesses() {
    try {
        const response = await fetch('/api/processes');
        const processes = await response.json();
        renderProcessList(processes);
        return processes;
    } catch (err) {
        console.error('Failed to fetch processes:', err);
        return [];
    }
}

// Render process list
function renderProcessList(processes) {
    const currentSelected = selectedProcess;
    processList.innerHTML = '';

    processes.forEach(proc => {
        const li = document.createElement('li');
        li.dataset.name = proc.name;

        if (proc.name === currentSelected) {
            li.classList.add('selected');
        }

        const indicator = document.createElement('span');
        indicator.className = `state-indicator state-${proc.state}`;

        const name = document.createElement('span');
        name.textContent = proc.name;

        li.appendChild(indicator);
        li.appendChild(name);

        li.addEventListener('click', () => selectProcess(proc.name));
        processList.appendChild(li);
    });

    updateButtons();
}

// Select a process
async function selectProcess(name) {
    selectedProcess = name;
    logTitle.textContent = name;
    logs = [];

    // Update UI
    document.querySelectorAll('#process-list li').forEach(li => {
        li.classList.toggle('selected', li.dataset.name === name);
    });

    updateButtons();

    // Fetch existing logs
    try {
        const response = await fetch(`/api/logs/${name}`);
        logs = await response.json();
        renderLogs();
    } catch (err) {
        console.error('Failed to fetch logs:', err);
    }

    // Note: We don't reconnect WebSocket here - we use a single connection
    // to "all" logs and filter client-side based on selectedProcess
}

// Connect WebSocket
function connectWebSocket() {
    // Clear any pending reconnect
    if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
        reconnectTimeout = null;
    }

    // Close existing connection
    if (ws) {
        intentionalClose = true;
        ws.close();
        ws = null;
    }

    const process = selectedProcess || 'all';
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${protocol}//${window.location.host}/ws/logs/${process}`);

    ws.onopen = () => {
        connectionStatus.textContent = 'Connected';
        connectionStatus.className = 'status connected';
        intentionalClose = false;
    };

    ws.onclose = () => {
        connectionStatus.textContent = 'Disconnected';
        connectionStatus.className = 'status disconnected';

        // Only reconnect if this wasn't an intentional close
        if (!intentionalClose) {
            reconnectTimeout = setTimeout(connectWebSocket, 3000);
        }
        intentionalClose = false;
    };

    ws.onmessage = (event) => {
        const log = JSON.parse(event.data);

        // Only add if it's for the selected process
        if (!selectedProcess || log.process === selectedProcess) {
            logs.push(log);

            // Limit log size
            if (logs.length > 10000) {
                logs = logs.slice(-5000);
            }

            appendLog(log);
        }
    };

    ws.onerror = (err) => {
        console.error('WebSocket error:', err);
    };
}

// Render all logs
function renderLogs() {
    logContent.innerHTML = '';

    const matchCount = logs.filter((_, i) => isMatch(logs[i])).length;

    logs.forEach((log, index) => {
        // Skip empty log lines
        if (!log.content || log.content.trim() === '') {
            return;
        }
        const line = createLogLine(log, index);
        logContent.appendChild(line);
    });

    updateStats(matchCount);

    if (autoScroll) {
        logViewer.scrollTop = logViewer.scrollHeight;
    }
}

// Append a single log line
function appendLog(log) {
    // Skip empty log lines
    if (!log.content || log.content.trim() === '') {
        return;
    }
    const line = createLogLine(log, logs.length - 1);
    logContent.appendChild(line);

    updateStats();

    if (autoScroll) {
        logViewer.scrollTop = logViewer.scrollHeight;
    }
}

// Create a log line element
function createLogLine(log, index) {
    const line = document.createElement('div');
    line.className = 'log-line';
    line.dataset.index = index;

    if (isMatch(log)) {
        line.classList.add('match');
    }

    const timestamp = document.createElement('span');
    timestamp.className = 'log-timestamp';
    timestamp.textContent = `[${log.timestamp}]`;

    const text = document.createElement('span');
    text.className = `log-text ${log.stream}`;

    // Convert ANSI codes to HTML
    const htmlContent = ansiUp.ansi_to_html(log.content);

    if (searchQuery && isMatch(log)) {
        text.innerHTML = highlightAnsiText(log.content);
    } else {
        text.innerHTML = htmlContent;
    }

    line.appendChild(timestamp);
    line.appendChild(text);

    return line;
}

// Check if log matches search
function isMatch(log) {
    if (!searchQuery) return false;
    return log.content.toLowerCase().includes(searchQuery.toLowerCase());
}

// Highlight matching text (for plain text)
function highlightText(text) {
    if (!searchQuery) return escapeHtml(text);

    const regex = new RegExp(`(${escapeRegex(searchQuery)})`, 'gi');
    return escapeHtml(text).replace(regex, '<span class="highlight">$1</span>');
}

// Highlight matching text in ANSI content
function highlightAnsiText(text) {
    // First convert ANSI to HTML
    const html = ansiUp.ansi_to_html(text);

    if (!searchQuery) return html;

    // Highlight matches (being careful not to match inside HTML tags)
    const regex = new RegExp(`(${escapeRegex(searchQuery)})(?![^<]*>)`, 'gi');
    return html.replace(regex, '<span class="highlight">$1</span>');
}

// Escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Escape regex special characters
function escapeRegex(string) {
    return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// Update stats
function updateStats(matchCount) {
    logStats.textContent = `${logs.length} lines`;

    if (searchQuery) {
        const count = matchCount ?? logs.filter(log => isMatch(log)).length;
        searchStats.textContent = `${count} matches`;
    } else {
        searchStats.textContent = '';
    }
}

// Update button states
function updateButtons() {
    const hasSelection = selectedProcess !== null;
    btnStart.disabled = !hasSelection;
    btnStop.disabled = !hasSelection;
    btnRestart.disabled = !hasSelection;
}

// Process actions
async function processAction(action) {
    if (!selectedProcess) return;

    try {
        await fetch(`/api/process/${selectedProcess}/${action}`, {
            method: 'POST'
        });

        // Refresh process list
        setTimeout(fetchProcesses, 500);
    } catch (err) {
        console.error(`Failed to ${action} process:`, err);
    }
}

// Download logs
function downloadLogs() {
    if (!selectedProcess || logs.length === 0) return;

    const content = logs.map(log => `[${log.timestamp}] ${log.content}`).join('\n');
    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);

    const a = document.createElement('a');
    a.href = url;
    a.download = `${selectedProcess}.log`;
    a.click();

    URL.revokeObjectURL(url);
}

// Setup event listeners
function setupEventListeners() {
    btnStart.addEventListener('click', () => processAction('start'));
    btnStop.addEventListener('click', () => processAction('stop'));
    btnRestart.addEventListener('click', () => processAction('restart'));
    btnClear.addEventListener('click', () => {
        logs = [];
        renderLogs();
    });
    btnDownload.addEventListener('click', downloadLogs);

    searchInput.addEventListener('input', (e) => {
        searchQuery = e.target.value;
        renderLogs();
    });

    // Disable auto-scroll when user scrolls up
    logViewer.addEventListener('scroll', () => {
        const atBottom = logViewer.scrollHeight - logViewer.scrollTop - logViewer.clientHeight < 50;
        autoScroll = atBottom;
    });

    // Keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        // Focus search on /
        if (e.key === '/' && document.activeElement !== searchInput) {
            e.preventDefault();
            searchInput.focus();
        }

        // Clear search on Escape
        if (e.key === 'Escape') {
            searchQuery = '';
            searchInput.value = '';
            renderLogs();
            searchInput.blur();
        }
    });
}

// Start the app
init();
