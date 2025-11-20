package api

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"

	"market/internal/v2/runtime"
)

// getRuntimeState handles GET /api/v2/runtime/state
func (s *Server) getRuntimeState(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/runtime/state - Getting runtime state")

	if s.runtimeStateService == nil {
		log.Println("Runtime state service not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Runtime state service not available", nil)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	filters := runtime.ParseFiltersFromQuery(
		query.Get("user_id"),
		query.Get("source_id"),
		query.Get("app_name"),
		query.Get("task_status"),
		query.Get("task_type"),
	)

	// Get snapshot with filters
	var snapshot *runtime.RuntimeSnapshot
	if filters.UserID != "" || filters.SourceID != "" || filters.AppName != "" ||
		filters.TaskStatus != "" || filters.TaskType != "" {
		snapshot = s.runtimeStateService.GetSnapshotWithFilters(filters)
	} else {
		snapshot = s.runtimeStateService.GetSnapshot()
	}

	log.Printf("Runtime state retrieved: %d apps, %d tasks, %d components",
		len(snapshot.AppStates), len(snapshot.Tasks), len(snapshot.Components))

	s.sendResponse(w, http.StatusOK, true, "Runtime state retrieved successfully", snapshot)
}

// getRuntimeDashboard handles GET /api/v2/runtime/dashboard
// Returns an HTML page with all runtime state information
func (s *Server) getRuntimeDashboard(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/runtime/dashboard - Getting runtime dashboard")

	if s.runtimeStateService == nil {
		log.Println("Runtime state service not initialized")
		http.Error(w, "Runtime state service not available", http.StatusInternalServerError)
		return
	}

	// Get full snapshot
	snapshot := s.runtimeStateService.GetSnapshot()
	if snapshot == nil {
		http.Error(w, "Failed to get runtime snapshot", http.StatusInternalServerError)
		return
	}

	// Convert snapshot to JSON for JavaScript consumption
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("Failed to marshal snapshot: %v", err)
		http.Error(w, "Failed to serialize snapshot", http.StatusInternalServerError)
		return
	}

	// Generate HTML page
	html := generateDashboardHTML(string(snapshotJSON))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// generateDashboardHTML generates the HTML dashboard page
func generateDashboardHTML(snapshotJSON string) string {
	// Escape JSON for embedding in HTML
	escapedJSON := template.JSEscapeString(snapshotJSON)

	htmlTemplate := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Runtime State Dashboard</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            min-height: 100vh;
            padding: 20px;
            color: #333;
        }
        
        .container {
            max-width: 1400px;
            margin: 0 auto;
        }
        
        .header {
            background: white;
            border-radius: 12px;
            padding: 24px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        
        .header h1 {
            color: #667eea;
            margin-bottom: 8px;
            font-size: 28px;
        }
        
        .header .timestamp {
            color: #666;
            font-size: 14px;
        }
        
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 20px;
        }
        
        .stat-card {
            background: white;
            border-radius: 12px;
            padding: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
            transition: transform 0.2s;
        }
        
        .stat-card:hover {
            transform: translateY(-4px);
            box-shadow: 0 6px 12px rgba(0, 0, 0, 0.15);
        }
        
        .stat-card .label {
            color: #666;
            font-size: 14px;
            margin-bottom: 8px;
        }
        
        .stat-card .value {
            color: #333;
            font-size: 32px;
            font-weight: bold;
        }
        
        .section {
            background: white;
            border-radius: 12px;
            padding: 24px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        
        .section h2 {
            color: #667eea;
            margin-bottom: 16px;
            font-size: 20px;
            border-bottom: 2px solid #667eea;
            padding-bottom: 8px;
        }
        
        .table-container {
            overflow-x: auto;
        }
        
        table {
            width: 100%%;
            border-collapse: collapse;
            margin-top: 12px;
        }
        
        th {
            background: #f8f9fa;
            padding: 12px;
            text-align: left;
            font-weight: 600;
            color: #333;
            border-bottom: 2px solid #dee2e6;
        }
        
        td {
            padding: 12px;
            border-bottom: 1px solid #dee2e6;
        }
        
        tr:hover {
            background: #f8f9fa;
        }
        
        .badge {
            display: inline-block;
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: 600;
        }
        
        .badge-success {
            background: #d4edda;
            color: #155724;
        }
        
        .badge-warning {
            background: #fff3cd;
            color: #856404;
        }
        
        .badge-danger {
            background: #f8d7da;
            color: #721c24;
        }
        
        .badge-info {
            background: #d1ecf1;
            color: #0c5460;
        }
        
        .badge-primary {
            background: #cfe2ff;
            color: #084298;
        }
        
        .progress-bar {
            width: 100%%;
            height: 8px;
            background: #e9ecef;
            border-radius: 4px;
            overflow: hidden;
            margin-top: 4px;
        }
        
        .progress-fill {
            height: 100%%;
            background: linear-gradient(90deg, #667eea 0%%, #764ba2 100%%);
            transition: width 0.3s;
        }
        
        .refresh-btn {
            background: #667eea;
            color: white;
            border: none;
            padding: 10px 20px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 14px;
            font-weight: 600;
            transition: background 0.2s;
        }
        
        .refresh-btn:hover {
            background: #5568d3;
        }
        
        .auto-refresh {
            display: flex;
            align-items: center;
            gap: 12px;
            margin-top: 12px;
        }
        
        .auto-refresh label {
            display: flex;
            align-items: center;
            gap: 8px;
            cursor: pointer;
        }
        
        .auto-refresh input[type="checkbox"] {
            width: 18px;
            height: 18px;
            cursor: pointer;
        }
        
        .json-viewer {
            background: #f8f9fa;
            border: 1px solid #dee2e6;
            border-radius: 6px;
            padding: 16px;
            max-height: 400px;
            overflow-y: auto;
            font-family: 'Courier New', monospace;
            font-size: 12px;
        }
        
        .tabs {
            display: flex;
            gap: 8px;
            margin-bottom: 16px;
            border-bottom: 2px solid #dee2e6;
        }
        
        .tab {
            padding: 12px 24px;
            cursor: pointer;
            border: none;
            background: none;
            font-size: 14px;
            font-weight: 600;
            color: #666;
            border-bottom: 3px solid transparent;
            transition: all 0.2s;
        }
        
        .tab:hover {
            color: #667eea;
        }
        
        .tab.active {
            color: #667eea;
            border-bottom-color: #667eea;
        }
        
        .tab-content {
            display: none;
        }
        
        .tab-content.active {
            display: block;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🚀 Runtime State Dashboard</h1>
            <div class="timestamp">Last Updated: <span id="timestamp"></span></div>
            <div class="auto-refresh">
                <button class="refresh-btn" onclick="refreshData()">🔄 Refresh</button>
                <label>
                    <input type="checkbox" id="autoRefresh" onchange="toggleAutoRefresh()">
                    Auto Refresh (5s)
                </label>
            </div>
        </div>
        
        <div class="stats-grid" id="statsGrid"></div>
        
        <div class="tabs">
            <button class="tab active" onclick="showTab('apps')">Applications</button>
            <button class="tab" onclick="showTab('tasks')">Tasks</button>
            <button class="tab" onclick="showTab('components')">Components</button>
            <button class="tab" onclick="showTab('chartrepo')">Chart Repo</button>
            <button class="tab" onclick="showTab('raw')">Raw JSON</button>
        </div>
        
        <div id="apps" class="tab-content active">
            <div class="section">
                <h2>Application States</h2>
                <div class="table-container">
                    <table id="appsTable">
                        <thead>
                            <tr>
                                <th>App Name</th>
                                <th>User ID</th>
                                <th>Source ID</th>
                                <th>Stage</th>
                                <th>Health</th>
                                <th>Version</th>
                                <th>Last Update</th>
                            </tr>
                        </thead>
                        <tbody id="appsTableBody"></tbody>
                    </table>
                </div>
            </div>
        </div>
        
        <div id="tasks" class="tab-content">
            <div class="section">
                <h2>Task States</h2>
                <div class="table-container">
                    <table id="tasksTable">
                        <thead>
                            <tr>
                                <th>Task ID</th>
                                <th>Type</th>
                                <th>Status</th>
                                <th>App Name</th>
                                <th>Progress</th>
                                <th>Created At</th>
                                <th>Error</th>
                            </tr>
                        </thead>
                        <tbody id="tasksTableBody"></tbody>
                    </table>
                </div>
            </div>
        </div>
        
        <div id="components" class="tab-content">
            <div class="section">
                <h2>Component Status</h2>
                <div class="table-container">
                    <table id="componentsTable">
                        <thead>
                            <tr>
                                <th>Component</th>
                                <th>Status</th>
                                <th>Healthy</th>
                                <th>Last Check</th>
                                <th>Message</th>
                            </tr>
                        </thead>
                        <tbody id="componentsTableBody"></tbody>
                    </table>
                </div>
            </div>
        </div>
        
        <div id="chartrepo" class="tab-content">
            <div class="section">
                <h2>Chart Repo - Applications</h2>
                <div class="table-container">
                    <table id="chartRepoAppsTable">
                        <thead>
                            <tr>
                                <th>App Name</th>
                                <th>User ID</th>
                                <th>Source ID</th>
                                <th>State</th>
                                <th>Current Step</th>
                                <th>Error</th>
                            </tr>
                        </thead>
                        <tbody id="chartRepoAppsTableBody"></tbody>
                    </table>
                </div>
            </div>
            <div class="section">
                <h2>Chart Repo - Images</h2>
                <div class="table-container">
                    <table id="chartRepoImagesTable">
                        <thead>
                            <tr>
                                <th>Image Name</th>
                                <th>App Name</th>
                                <th>Status</th>
                                <th>Progress</th>
                                <th>Size</th>
                            </tr>
                        </thead>
                        <tbody id="chartRepoImagesTableBody"></tbody>
                    </table>
                </div>
            </div>
            <div class="section">
                <h2>Chart Repo - Tasks</h2>
                <div class="table-container">
                    <table id="chartRepoTasksTable">
                        <thead>
                            <tr>
                                <th>Task ID</th>
                                <th>App Name</th>
                                <th>Status</th>
                                <th>Step</th>
                                <th>Retry Count</th>
                                <th>Error</th>
                            </tr>
                        </thead>
                        <tbody id="chartRepoTasksTableBody"></tbody>
                    </table>
                </div>
            </div>
        </div>
        
        <div id="raw" class="tab-content">
            <div class="section">
                <h2>Raw JSON Data</h2>
                <div class="json-viewer">
                    <pre id="jsonViewer"></pre>
                </div>
            </div>
        </div>
    </div>
    
    <script>
        let snapshotData = %s;
        let autoRefreshInterval = null;
        
        function formatTimestamp(timestamp) {
            if (!timestamp) return 'N/A';
            const date = new Date(timestamp);
            return date.toLocaleString('zh-CN');
        }
        
        function formatDuration(ms) {
            if (!ms) return 'N/A';
            const seconds = Math.floor(ms / 1000);
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);
            if (hours > 0) return hours + 'h ' + (minutes %% 60) + 'm';
            if (minutes > 0) return minutes + 'm ' + (seconds %% 60) + 's';
            return seconds + 's';
        }
        
        function formatBytes(bytes) {
            if (!bytes) return 'N/A';
            const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
            if (bytes === 0) return '0 B';
            const i = Math.floor(Math.log(bytes) / Math.log(1024));
            return Math.round(bytes / Math.pow(1024, i) * 100) / 100 + ' ' + sizes[i];
        }
        
        function getStatusBadge(status) {
            const statusLower = (status || '').toLowerCase();
            if (statusLower.includes('running') || statusLower.includes('healthy') || statusLower === 'completed') {
                return '<span class="badge badge-success">' + status + '</span>';
            } else if (statusLower.includes('pending') || statusLower.includes('processing')) {
                return '<span class="badge badge-warning">' + status + '</span>';
            } else if (statusLower.includes('failed') || statusLower.includes('unhealthy') || statusLower.includes('error')) {
                return '<span class="badge badge-danger">' + status + '</span>';
            }
            return '<span class="badge badge-info">' + status + '</span>';
        }
        
        function renderStats() {
            const summary = snapshotData.summary || {};
            const statsGrid = document.getElementById('statsGrid');
            statsGrid.innerHTML = 
                '<div class="stat-card">' +
                    '<div class="label">Total Apps</div>' +
                    '<div class="value">' + (summary.total_apps || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Total Tasks</div>' +
                    '<div class="value">' + (summary.total_tasks || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Running Tasks</div>' +
                    '<div class="value">' + (summary.running_tasks || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Pending Tasks</div>' +
                    '<div class="value">' + (summary.pending_tasks || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Failed Tasks</div>' +
                    '<div class="value">' + (summary.failed_tasks || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Healthy Apps</div>' +
                    '<div class="value">' + (summary.healthy_apps || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Active Components</div>' +
                    '<div class="value">' + (summary.active_components || 0) + '</div>' +
                '</div>';
        }
        
        function renderApps() {
            const apps = snapshotData.app_states || {};
            const tbody = document.getElementById('appsTableBody');
            tbody.innerHTML = '';
            
            Object.values(apps).forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.stage || 'unknown') + '</td>' +
                    '<td>' + getStatusBadge(app.health || 'unknown') + '</td>' +
                    '<td>' + (app.version || 'N/A') + '</td>' +
                    '<td>' + formatTimestamp(app.last_update) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasks() {
            const tasks = snapshotData.tasks || {};
            const tbody = document.getElementById('tasksTableBody');
            tbody.innerHTML = '';
            
            Object.values(tasks).forEach(task => {
                const progress = task.progress || 0;
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.type || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td><div class="progress-bar"><div class="progress-fill" style="width: ' + progress + '%%"></div></div>' + progress + '%%</td>' +
                    '<td>' + formatTimestamp(task.created_at) + '</td>' +
                    '<td>' + (task.error_msg || '-') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderComponents() {
            const components = snapshotData.components || {};
            const tbody = document.getElementById('componentsTableBody');
            tbody.innerHTML = '';
            
            Object.entries(components).forEach(([name, comp]) => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + name + '</td>' +
                    '<td>' + getStatusBadge(comp.status || 'unknown') + '</td>' +
                    '<td>' + getStatusBadge(comp.healthy ? 'healthy' : 'unhealthy') + '</td>' +
                    '<td>' + formatTimestamp(comp.last_check) + '</td>' +
                    '<td>' + (comp.message || '-') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderChartRepo() {
            const chartRepo = snapshotData.chart_repo || {};
            
            // Chart Repo Apps
            const apps = chartRepo.apps || [];
            const appsTbody = document.getElementById('chartRepoAppsTableBody');
            appsTbody.innerHTML = '';
            apps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.state || 'unknown') + '</td>' +
                    '<td>' + (app.current_step ? app.current_step.name : '-') + '</td>' +
                    '<td>' + (app.error ? app.error.message : '-') + '</td>';
                appsTbody.appendChild(row);
            });
            
            // Chart Repo Images
            const images = chartRepo.images || [];
            const imagesTbody = document.getElementById('chartRepoImagesTableBody');
            imagesTbody.innerHTML = '';
            images.forEach(image => {
                const progress = image.download_progress || 0;
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (image.image_name || 'N/A') + '</td>' +
                    '<td>' + (image.app_name || '-') + '</td>' +
                    '<td>' + getStatusBadge(image.status || 'unknown') + '</td>' +
                    '<td><div class="progress-bar"><div class="progress-fill" style="width: ' + progress.toFixed(2) + '%%"></div></div>' + progress.toFixed(2) + '%%</td>' +
                    '<td>' + formatBytes(image.total_size) + '</td>';
                imagesTbody.appendChild(row);
            });
            
            // Chart Repo Tasks
            const hydrator = chartRepo.tasks && chartRepo.tasks.hydrator;
            const tasks = hydrator ? (hydrator.tasks || []) : [];
            const tasksTbody = document.getElementById('chartRepoTasksTableBody');
            tasksTbody.innerHTML = '';
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.step_name || '-') + '</td>' +
                    '<td>' + (task.retry_count || 0) + '</td>' +
                    '<td>' + (task.last_error || '-') + '</td>';
                tasksTbody.appendChild(row);
            });
        }
        
        function renderRawJSON() {
            const viewer = document.getElementById('jsonViewer');
            viewer.textContent = JSON.stringify(snapshotData, null, 2);
        }
        
        function showTab(tabName) {
            // Hide all tabs
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
            });
            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.remove('active');
            });
            
            // Show selected tab
            document.getElementById(tabName).classList.add('active');
            event.target.classList.add('active');
            
            // Render content based on tab
            if (tabName === 'apps') renderApps();
            else if (tabName === 'tasks') renderTasks();
            else if (tabName === 'components') renderComponents();
            else if (tabName === 'chartrepo') renderChartRepo();
            else if (tabName === 'raw') renderRawJSON();
        }
        
        function refreshData() {
            fetch('/app-store/api/v2/runtime/state')
                .then(response => response.json())
                .then(data => {
                    if (data.success && data.data) {
                        snapshotData = data.data;
                        updateUI();
                    }
                })
                .catch(error => {
                    console.error('Failed to refresh data:', error);
                    alert('Failed to refresh data. Please try again.');
                });
        }
        
        function updateUI() {
            document.getElementById('timestamp').textContent = formatTimestamp(snapshotData.timestamp);
            renderStats();
            renderApps();
            renderTasks();
            renderComponents();
            renderChartRepo();
            renderRawJSON();
        }
        
        function toggleAutoRefresh() {
            const checkbox = document.getElementById('autoRefresh');
            if (checkbox.checked) {
                autoRefreshInterval = setInterval(refreshData, 5000);
            } else {
                if (autoRefreshInterval) {
                    clearInterval(autoRefreshInterval);
                    autoRefreshInterval = null;
                }
            }
        }
        
        // Initial render
        updateUI();
    </script>
</body>
</html>`

	// Replace the placeholder with actual JSON data
	return strings.Replace(htmlTemplate, "%s", escapedJSON, 1)
}
