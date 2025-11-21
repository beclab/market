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
            background: #f5f7fa;
            min-height: 100vh;
            padding: 16px;
            color: #1a1a1a;
        }
        
        .container {
            max-width: 100%;
            margin: 0 auto;
        }
        
        .header {
            background: #ffffff;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 16px;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.08);
        }
        
        .header h1 {
            color: #1a1a1a;
            margin-bottom: 12px;
            font-size: 24px;
            font-weight: 600;
        }
        
        .header .timestamp {
            color: #666666;
            font-size: 14px;
            margin-bottom: 12px;
        }
        
        .header .controls {
            display: flex;
            align-items: center;
            gap: 12px;
        }
        
        .refresh-btn {
            background: #2563eb;
            color: white;
            border: none;
            padding: 8px 16px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 14px;
            font-weight: 500;
            transition: background 0.2s;
        }
        
        .refresh-btn:hover {
            background: #1d4ed8;
        }
        
        .auto-refresh label {
            display: flex;
            align-items: center;
            gap: 8px;
            cursor: pointer;
            color: #666666;
            font-size: 14px;
        }
        
        .auto-refresh input[type="checkbox"] {
            width: 16px;
            height: 16px;
            cursor: pointer;
        }
        
        .main-layout {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 16px;
        }
        
        .panel {
            background: #ffffff;
            border-radius: 8px;
            padding: 20px;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.08);
            display: flex;
            flex-direction: column;
        }
        
        .panel-header {
            margin-bottom: 16px;
            padding-bottom: 12px;
            border-bottom: 2px solid #e5e7eb;
        }
        
        .panel-header h2 {
            color: #1a1a1a;
            font-size: 20px;
            font-weight: 600;
        }
        
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            gap: 12px;
            margin-bottom: 16px;
        }
        
        .stat-card {
            background: #f9fafb;
            border-radius: 6px;
            padding: 16px;
            border: 1px solid #e5e7eb;
        }
        
        .stat-card .label {
            color: #666666;
            font-size: 12px;
            margin-bottom: 8px;
            font-weight: 500;
        }
        
        .stat-card .value {
            color: #1a1a1a;
            font-size: 24px;
            font-weight: 700;
        }
        
        .section {
            margin-bottom: 20px;
        }
        
        .section h3 {
            color: #1a1a1a;
            margin-bottom: 12px;
            font-size: 16px;
            font-weight: 600;
        }
        
        .table-container {
            overflow-x: auto;
            border: 1px solid #e5e7eb;
            border-radius: 6px;
        }
        
        table {
            width: 100%;
            border-collapse: collapse;
            font-size: 13px;
        }
        
        th {
            background: #f9fafb;
            padding: 10px 12px;
            text-align: left;
            font-weight: 600;
            color: #1a1a1a;
            border-bottom: 2px solid #e5e7eb;
            white-space: nowrap;
        }
        
        td {
            padding: 10px 12px;
            border-bottom: 1px solid #e5e7eb;
            color: #1a1a1a;
        }
        
        tr:hover {
            background: #f9fafb;
        }
        
        .badge {
            display: inline-block;
            padding: 4px 10px;
            border-radius: 4px;
            font-size: 11px;
            font-weight: 600;
            white-space: nowrap;
        }
        
        .badge-success {
            background: #d1fae5;
            color: #065f46;
        }
        
        .badge-warning {
            background: #fef3c7;
            color: #92400e;
        }
        
        .badge-danger {
            background: #fee2e2;
            color: #991b1b;
        }
        
        .badge-info {
            background: #dbeafe;
            color: #1e40af;
        }
        
        .progress-bar {
            width: 100%;
            height: 6px;
            background: #e5e7eb;
            border-radius: 3px;
            overflow: hidden;
            margin-top: 4px;
        }
        
        .progress-fill {
            height: 100%;
            background: #2563eb;
            transition: width 0.3s;
        }
        
        .empty-state {
            text-align: center;
            padding: 40px 20px;
            color: #9ca3af;
            font-size: 14px;
        }
        
        @media (max-width: 1200px) {
            .main-layout {
                grid-template-columns: 1fr;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Runtime State Dashboard</h1>
            <div class="timestamp">Last Updated: <span id="timestamp"></span></div>
            <div class="controls">
                <button class="refresh-btn" onclick="refreshData()">Refresh</button>
                <div class="auto-refresh">
                    <label>
                        <input type="checkbox" id="autoRefresh" onchange="toggleAutoRefresh()" checked>
                        Auto Refresh (5s)
                    </label>
                </div>
            </div>
        </div>
        
        <div class="main-layout">
            <div class="panel">
                <div class="panel-header">
                    <h2>Market Status</h2>
                </div>
                <div class="stats-grid" id="marketStatsGrid"></div>
                <div class="section">
                    <h3>Applications</h3>
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
                                </tr>
                            </thead>
                            <tbody id="appsTableBody"></tbody>
                        </table>
                    </div>
                </div>
                <div class="section">
                    <h3>Tasks</h3>
                    <div class="table-container">
                        <table id="tasksTable">
                            <thead>
                                <tr>
                                    <th>Task ID</th>
                                    <th>Type</th>
                                    <th>Status</th>
                                    <th>App Name</th>
                                    <th>Progress</th>
                                </tr>
                            </thead>
                            <tbody id="tasksTableBody"></tbody>
                        </table>
                    </div>
                </div>
                <div class="section">
                    <h3>Components</h3>
                    <div class="table-container">
                        <table id="componentsTable">
                            <thead>
                                <tr>
                                    <th>Component</th>
                                    <th>Status</th>
                                    <th>Healthy</th>
                                    <th>Last Check</th>
                                </tr>
                            </thead>
                            <tbody id="componentsTableBody"></tbody>
                        </table>
                    </div>
                </div>
            </div>
            
            <div class="panel">
                <div class="panel-header">
                    <h2>Chart Repo Status</h2>
                </div>
                <div class="stats-grid" id="chartRepoStatsGrid"></div>
                <div class="section">
                    <h3>Applications</h3>
                    <div class="table-container">
                        <table id="chartRepoAppsTable">
                            <thead>
                                <tr>
                                    <th>App Name</th>
                                    <th>User ID</th>
                                    <th>Source ID</th>
                                    <th>State</th>
                                    <th>Current Step</th>
                                </tr>
                            </thead>
                            <tbody id="chartRepoAppsTableBody"></tbody>
                        </table>
                    </div>
                </div>
                <div class="section">
                    <h3>Images</h3>
                    <div class="table-container">
                        <table id="chartRepoImagesTable">
                            <thead>
                                <tr>
                                    <th>Image Name</th>
                                    <th>App Name</th>
                                    <th>Status</th>
                                    <th>Progress</th>
                                </tr>
                            </thead>
                            <tbody id="chartRepoImagesTableBody"></tbody>
                        </table>
                    </div>
                </div>
                <div class="section">
                    <h3>Tasks</h3>
                    <div class="table-container">
                        <table id="chartRepoTasksTable">
                            <thead>
                                <tr>
                                    <th>Task ID</th>
                                    <th>App Name</th>
                                    <th>Status</th>
                                    <th>Step</th>
                                    <th>Retry</th>
                                </tr>
                            </thead>
                            <tbody id="chartRepoTasksTableBody"></tbody>
                        </table>
                    </div>
                </div>
            </div>
        </div>
    </div>
    
    <script>
        let snapshotData = {};
        let autoRefreshInterval = null;
        
        try {
            snapshotData = JSON.parse('%s');
        } catch (e) {
            console.error('Failed to parse snapshot data:', e);
            snapshotData = {};
        }
        
        function formatTimestamp(timestamp) {
            if (!timestamp) return 'N/A';
            const date = new Date(timestamp);
            return date.toLocaleString('zh-CN');
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
        
        function renderMarketStats() {
            const summary = snapshotData.summary || {};
            const statsGrid = document.getElementById('marketStatsGrid');
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
                '</div>';
        }
        
        function renderChartRepoStats() {
            const chartRepo = snapshotData.chart_repo || {};
            const hydrator = chartRepo.tasks && chartRepo.tasks.hydrator;
            const statsGrid = document.getElementById('chartRepoStatsGrid');
            statsGrid.innerHTML = 
                '<div class="stat-card">' +
                    '<div class="label">Total Apps</div>' +
                    '<div class="value">' + ((chartRepo.apps && chartRepo.apps.length) || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Total Images</div>' +
                    '<div class="value">' + ((chartRepo.images && chartRepo.images.length) || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Active Tasks</div>' +
                    '<div class="value">' + (hydrator ? (hydrator.active_tasks || 0) : 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Queue Length</div>' +
                    '<div class="value">' + (hydrator ? (hydrator.queue_length || 0) : 0) + '</div>' +
                '</div>';
        }
        
        function renderApps() {
            const apps = snapshotData.app_states || {};
            const tbody = document.getElementById('appsTableBody');
            tbody.innerHTML = '';
            
            const appList = Object.values(apps);
            if (appList.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No applications</td></tr>';
                return;
            }
            
            appList.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.stage || 'unknown') + '</td>' +
                    '<td>' + getStatusBadge(app.health || 'unknown') + '</td>' +
                    '<td>' + (app.version || 'N/A') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasks() {
            const tasks = snapshotData.tasks || {};
            const tbody = document.getElementById('tasksTableBody');
            tbody.innerHTML = '';
            
            const taskList = Object.values(tasks);
            if (taskList.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No tasks</td></tr>';
                return;
            }
            
            taskList.forEach(task => {
                const progress = task.progress || 0;
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.type || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td><div class="progress-bar"><div class="progress-fill" style="width: ' + progress + '%"></div></div></td>';
                tbody.appendChild(row);
            });
        }
        
        function renderComponents() {
            const components = snapshotData.components || {};
            const tbody = document.getElementById('componentsTableBody');
            tbody.innerHTML = '';
            
            const compList = Object.entries(components);
            if (compList.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No components</td></tr>';
                return;
            }
            
            compList.forEach(([name, comp]) => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + name + '</td>' +
                    '<td>' + getStatusBadge(comp.status || 'unknown') + '</td>' +
                    '<td>' + getStatusBadge(comp.healthy ? 'healthy' : 'unhealthy') + '</td>' +
                    '<td>' + formatTimestamp(comp.last_check) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderChartRepo() {
            const chartRepo = snapshotData.chart_repo || {};
            
            const apps = chartRepo.apps || [];
            const appsTbody = document.getElementById('chartRepoAppsTableBody');
            appsTbody.innerHTML = '';
            if (apps.length === 0) {
                appsTbody.innerHTML = '<tr><td colspan="5" class="empty-state">No applications</td></tr>';
            } else {
                apps.forEach(app => {
                    const row = document.createElement('tr');
                    row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                        '<td>' + (app.user_id || 'N/A') + '</td>' +
                        '<td>' + (app.source_id || 'N/A') + '</td>' +
                        '<td>' + getStatusBadge(app.state || 'unknown') + '</td>' +
                        '<td>' + (app.current_step ? app.current_step.name : '-') + '</td>';
                    appsTbody.appendChild(row);
                });
            }
            
            const images = chartRepo.images || [];
            const imagesTbody = document.getElementById('chartRepoImagesTableBody');
            imagesTbody.innerHTML = '';
            if (images.length === 0) {
                imagesTbody.innerHTML = '<tr><td colspan="4" class="empty-state">No images</td></tr>';
            } else {
                images.forEach(image => {
                    const progress = image.download_progress || 0;
                    const row = document.createElement('tr');
                    row.innerHTML = '<td style="font-size: 11px;">' + (image.image_name || 'N/A') + '</td>' +
                        '<td>' + (image.app_name || '-') + '</td>' +
                        '<td>' + getStatusBadge(image.status || 'unknown') + '</td>' +
                        '<td><div class="progress-bar"><div class="progress-fill" style="width: ' + progress.toFixed(2) + '%"></div></div></td>';
                    imagesTbody.appendChild(row);
                });
            }
            
            const hydrator = chartRepo.tasks && chartRepo.tasks.hydrator;
            const tasks = hydrator ? (hydrator.tasks || []) : [];
            const tasksTbody = document.getElementById('chartRepoTasksTableBody');
            tasksTbody.innerHTML = '';
            if (tasks.length === 0) {
                tasksTbody.innerHTML = '<tr><td colspan="5" class="empty-state">No tasks</td></tr>';
            } else {
                tasks.forEach(task => {
                    const row = document.createElement('tr');
                    row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                        '<td>' + (task.app_name || 'N/A') + '</td>' +
                        '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                        '<td>' + (task.step_name || '-') + '</td>' +
                        '<td>' + (task.retry_count || 0) + '</td>';
                    tasksTbody.appendChild(row);
                });
            }
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
                });
        }
        
        function updateUI() {
            document.getElementById('timestamp').textContent = formatTimestamp(snapshotData.timestamp);
            renderMarketStats();
            renderChartRepoStats();
            renderApps();
            renderTasks();
            renderComponents();
            renderChartRepo();
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
        
        updateUI();
        if (document.getElementById('autoRefresh').checked) {
            autoRefreshInterval = setInterval(refreshData, 5000);
        }
    </script>
</body>
</html>`

	// Replace the placeholder with actual JSON data
	return strings.Replace(htmlTemplate, "%s", escapedJSON, 1)
}
