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

	// Debug: log all app states to verify stage field
	if snapshot != nil && len(snapshot.AppStates) > 0 {
		log.Printf("[DEBUG] Dashboard - Total apps: %d", len(snapshot.AppStates))
		for key, appState := range snapshot.AppStates {
			if appState != nil {
				log.Printf("[DEBUG] Dashboard - App key: %s, AppName: %s, Stage: %s (type: %T), Health: %s",
					key, appState.AppName, appState.Stage, appState.Stage, appState.Health)
			} else {
				log.Printf("[DEBUG] Dashboard - App key: %s, AppState is nil", key)
			}
		}
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
        
        .main-tabs {
            display: flex;
            gap: 8px;
            margin-bottom: 16px;
            border-bottom: 2px solid #e5e7eb;
        }
        
        .main-tab {
            padding: 12px 24px;
            cursor: pointer;
            border: none;
            background: none;
            font-size: 16px;
            font-weight: 600;
            color: #666666;
            border-bottom: 3px solid transparent;
            transition: all 0.2s;
        }
        
        .main-tab:hover {
            color: #2563eb;
        }
        
        .main-tab.active {
            color: #2563eb;
            border-bottom-color: #2563eb;
        }
        
        .main-tab-content {
            display: none;
        }
        
        .main-tab-content.active {
            display: block;
        }
        
        .sub-tabs {
            display: flex;
            gap: 4px;
            margin-bottom: 12px;
            flex-wrap: wrap;
        }
        
        .sub-tab {
            padding: 8px 16px;
            cursor: pointer;
            border: none;
            background: #f9fafb;
            font-size: 13px;
            font-weight: 500;
            color: #666666;
            border-radius: 4px;
            transition: all 0.2s;
        }
        
        .sub-tab:hover {
            background: #e5e7eb;
            color: #1a1a1a;
        }
        
        .sub-tab.active {
            background: #2563eb;
            color: white;
        }
        
        .sub-tab-content {
            display: none;
        }
        
        .sub-tab-content.active {
            display: block;
        }
        
        .error-modal {
            display: none;
            position: fixed;
            z-index: 1000;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background-color: rgba(0, 0, 0, 0.5);
        }
        
        .error-modal-content {
            background-color: #ffffff;
            margin: 10% auto;
            padding: 20px;
            border: 1px solid #e5e7eb;
            border-radius: 8px;
            width: 80%;
            max-width: 800px;
            max-height: 70vh;
            overflow-y: auto;
        }
        
        .error-modal-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 16px;
            padding-bottom: 12px;
            border-bottom: 2px solid #e5e7eb;
        }
        
        .error-modal-header h3 {
            margin: 0;
            color: #1a1a1a;
        }
        
        .error-modal-close {
            background: none;
            border: none;
            font-size: 24px;
            cursor: pointer;
            color: #666666;
        }
        
        .error-modal-close:hover {
            color: #1a1a1a;
        }
        
        .error-modal-body {
            color: #1a1a1a;
            font-family: monospace;
            font-size: 12px;
            white-space: pre-wrap;
            word-wrap: break-word;
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
                
                <!-- Main Tabs for Applications, Tasks, Components, etc. -->
                <div class="main-tabs">
                    <button class="main-tab active" onclick="showMainTab('marketApps', this)">Applications</button>
                    <button class="main-tab" onclick="showMainTab('marketTasks', this)">Tasks</button>
                    <button class="main-tab" onclick="showMainTab('marketComponents', this)">Components</button>
                    <button class="main-tab" onclick="showMainTab('marketCache', this)">Cache & Sync</button>
                </div>
                
                <!-- Applications Tab Content -->
                <div id="marketApps" class="main-tab-content active">
                    <!-- Sub-tabs for Applications by stage -->
                    <div class="sub-tabs">
                        <button class="sub-tab active" onclick="showSubTab('marketApps', 'all', this)">All</button>
                        <button class="sub-tab" onclick="showSubTab('marketApps', 'fetching', this)">Fetching</button>
                        <button class="sub-tab" onclick="showSubTab('marketApps', 'installing', this)">Installing</button>
                        <button class="sub-tab" onclick="showSubTab('marketApps', 'running', this)">Running</button>
                        <button class="sub-tab" onclick="showSubTab('marketApps', 'upgrading', this)">Upgrading</button>
                        <button class="sub-tab" onclick="showSubTab('marketApps', 'failed', this)">Failed</button>
                    </div>
                    <div class="sub-tab-content active" id="marketApps-all">
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
                    <div class="sub-tab-content" id="marketApps-fetching">
                        <div class="table-container">
                            <table>
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
                                <tbody id="appsFetchingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketApps-installing">
                        <div class="table-container">
                            <table>
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
                                <tbody id="appsInstallingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketApps-running">
                        <div class="table-container">
                            <table>
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
                                <tbody id="appsRunningBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketApps-upgrading">
                        <div class="table-container">
                            <table>
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
                                <tbody id="appsUpgradingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketApps-failed">
                        <div class="table-container">
                            <table>
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
                                <tbody id="appsFailedBody"></tbody>
                            </table>
                        </div>
                    </div>
                </div>
                
                <!-- Tasks Tab Content -->
                <div id="marketTasks" class="main-tab-content">
                    <!-- Sub-tabs for Tasks by status -->
                    <div class="sub-tabs">
                        <button class="sub-tab active" onclick="showSubTab('marketTasks', 'all', this)">All</button>
                        <button class="sub-tab" onclick="showSubTab('marketTasks', 'pending', this)">Pending</button>
                        <button class="sub-tab" onclick="showSubTab('marketTasks', 'running', this)">Running</button>
                        <button class="sub-tab" onclick="showSubTab('marketTasks', 'completed', this)">Completed</button>
                        <button class="sub-tab" onclick="showSubTab('marketTasks', 'failed', this)">Failed</button>
                    </div>
                    <div class="sub-tab-content active" id="marketTasks-all">
                        <div class="table-container">
                            <table id="tasksTable">
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>Type</th>
                                        <th>Status</th>
                                        <th>App Name</th>
                                        <th>Created At</th>
                                        <th>Updated At</th>
                                        <th>Error</th>
                                    </tr>
                                </thead>
                                <tbody id="tasksTableBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketTasks-pending">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>Type</th>
                                        <th>Status</th>
                                        <th>App Name</th>
                                        <th>Created At</th>
                                    </tr>
                                </thead>
                                <tbody id="tasksPendingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketTasks-running">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>Type</th>
                                        <th>Status</th>
                                        <th>App Name</th>
                                        <th>Started At</th>
                                    </tr>
                                </thead>
                                <tbody id="tasksRunningBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketTasks-completed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>Type</th>
                                        <th>Status</th>
                                        <th>App Name</th>
                                        <th>Completed At</th>
                                    </tr>
                                </thead>
                                <tbody id="tasksCompletedBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="marketTasks-failed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>Type</th>
                                        <th>Status</th>
                                        <th>App Name</th>
                                        <th>Error</th>
                                        <th>Failed At</th>
                                    </tr>
                                </thead>
                                <tbody id="tasksFailedBody"></tbody>
                            </table>
                        </div>
                    </div>
                </div>
                
                <!-- Components Tab Content -->
                <div id="marketComponents" class="main-tab-content">
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
                
                <!-- Cache & Sync Tab Content -->
                <div id="marketCache" class="main-tab-content">
                    <!-- Sub-tabs for Cache & Sync -->
                    <div class="sub-tabs">
                        <button class="sub-tab active" onclick="showSubTab('marketCache', 'syncer', this)">Syncer</button>
                        <button class="sub-tab" onclick="showSubTab('marketCache', 'hydrator', this)">Hydrator</button>
                        <button class="sub-tab" onclick="showSubTab('marketCache', 'cache', this)">Cache Statistics</button>
                    </div>
                    
                    <!-- Syncer Tab Content -->
                    <div class="sub-tab-content active" id="marketCache-syncer">
                        <div class="table-container">
                            <table id="syncerTable">
                                <thead>
                                    <tr>
                                        <th>Property</th>
                                        <th>Value</th>
                                    </tr>
                                </thead>
                                <tbody id="syncerTableBody"></tbody>
                            </table>
                        </div>
                    </div>
                    
                    <!-- Hydrator Tab Content -->
                    <div class="sub-tab-content" id="marketCache-hydrator">
                        <div class="table-container">
                            <table id="hydratorTable">
                                <thead>
                                    <tr>
                                        <th>Property</th>
                                        <th>Value</th>
                                    </tr>
                                </thead>
                                <tbody id="hydratorTableBody"></tbody>
                            </table>
                        </div>
                        <!-- Active Tasks Section -->
                        <div id="hydratorActiveTasks" style="margin-top: 20px;"></div>
                        <!-- Worker Status Section -->
                        <div id="hydratorWorkers" style="margin-top: 20px;"></div>
                        <!-- Task History Section -->
                        <div id="hydratorHistory" style="margin-top: 20px;"></div>
                    </div>
                    
                    <!-- Cache Statistics Tab Content -->
                    <div class="sub-tab-content" id="marketCache-cache">
                        <div class="table-container">
                            <table id="cacheStatsTable">
                                <thead>
                                    <tr>
                                        <th>Metric</th>
                                        <th>Value</th>
                                    </tr>
                                </thead>
                                <tbody id="cacheStatsTableBody"></tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </div>
            
            <div class="panel">
                <div class="panel-header">
                    <h2>Chart Repo Status</h2>
                </div>
                <div class="stats-grid" id="chartRepoStatsGrid"></div>
                
                <!-- Main Tabs for Applications, Images, Tasks -->
                <div class="main-tabs">
                    <button class="main-tab active" onclick="showMainTab('chartRepoApps', this)">Applications</button>
                    <button class="main-tab" onclick="showMainTab('chartRepoImageAnalyzer', this)">Image Analyzer</button>
                    <button class="main-tab" onclick="showMainTab('chartRepoTasks', this)">Tasks</button>
                </div>
                
                <!-- Applications Tab Content -->
                <div id="chartRepoApps" class="main-tab-content active">
                    <!-- Sub-tabs for Applications by state -->
                    <div class="sub-tabs">
                        <button class="sub-tab active" onclick="showSubTab('chartRepoApps', 'all', this)">All</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoApps', 'processing', this)">Processing</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoApps', 'completed', this)">Completed</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoApps', 'failed', this)">Failed</button>
                    </div>
                    <div class="sub-tab-content active" id="chartRepoApps-all">
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
                    <div class="sub-tab-content" id="chartRepoApps-processing">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>App Name</th>
                                        <th>User ID</th>
                                        <th>Source ID</th>
                                        <th>State</th>
                                        <th>Current Step</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoAppsProcessingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="chartRepoApps-completed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>App Name</th>
                                        <th>User ID</th>
                                        <th>Source ID</th>
                                        <th>State</th>
                                        <th>Current Step</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoAppsCompletedBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="chartRepoApps-failed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>App Name</th>
                                        <th>User ID</th>
                                        <th>Source ID</th>
                                        <th>State</th>
                                        <th>Error</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoAppsFailedBody"></tbody>
                            </table>
                        </div>
                    </div>
                </div>
                
                <!-- Image Analyzer Tab Content -->
                <div id="chartRepoImageAnalyzer" class="main-tab-content">
                    <!-- Sub-tabs for Image Analyzer -->
                    <div class="sub-tabs">
                        <button class="sub-tab active" onclick="showSubTab('chartRepoImageAnalyzer', 'overview', this)">Overview</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoImageAnalyzer', 'queued', this)">Queued Tasks</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoImageAnalyzer', 'processing', this)">Processing</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoImageAnalyzer', 'completed', this)">Completed</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoImageAnalyzer', 'failed', this)">Failed</button>
                    </div>
                    
                    <!-- Overview Tab Content -->
                    <div class="sub-tab-content active" id="chartRepoImageAnalyzer-overview">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Property</th>
                                        <th>Value</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoImageAnalyzerOverviewBody"></tbody>
                            </table>
                        </div>
                    </div>
                    
                    <!-- Queued Tasks Tab Content -->
                    <div class="sub-tab-content" id="chartRepoImageAnalyzer-queued">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Images Count</th>
                                        <th>Created At</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoImageAnalyzerQueuedBody"></tbody>
                            </table>
                        </div>
                    </div>
                    
                    <!-- Processing Tasks Tab Content -->
                    <div class="sub-tab-content" id="chartRepoImageAnalyzer-processing">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Worker ID</th>
                                        <th>Progress</th>
                                        <th>Started At</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoImageAnalyzerProcessingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    
                    <!-- Completed Tasks Tab Content -->
                    <div class="sub-tab-content" id="chartRepoImageAnalyzer-completed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Duration</th>
                                        <th>Completed At</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoImageAnalyzerCompletedBody"></tbody>
                            </table>
                        </div>
                    </div>
                    
                    <!-- Failed Tasks Tab Content -->
                    <div class="sub-tab-content" id="chartRepoImageAnalyzer-failed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Error</th>
                                        <th>Error Step</th>
                                        <th>Failed At</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoImageAnalyzerFailedBody"></tbody>
                            </table>
                        </div>
                    </div>
                </div>
                
                <!-- Tasks Tab Content -->
                <div id="chartRepoTasks" class="main-tab-content">
                    <!-- Sub-tabs for Tasks by status -->
                    <div class="sub-tabs">
                        <button class="sub-tab active" onclick="showSubTab('chartRepoTasks', 'all', this)">All</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoTasks', 'pending', this)">Pending</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoTasks', 'running', this)">Running</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoTasks', 'completed', this)">Completed</button>
                        <button class="sub-tab" onclick="showSubTab('chartRepoTasks', 'failed', this)">Failed</button>
                    </div>
                    <div class="sub-tab-content active" id="chartRepoTasks-all">
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
                    <div class="sub-tab-content" id="chartRepoTasks-pending">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Step</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoTasksPendingBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="chartRepoTasks-running">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Step</th>
                                        <th>Retry</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoTasksRunningBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="chartRepoTasks-completed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Step</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoTasksCompletedBody"></tbody>
                            </table>
                        </div>
                    </div>
                    <div class="sub-tab-content" id="chartRepoTasks-failed">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Task ID</th>
                                        <th>App Name</th>
                                        <th>Status</th>
                                        <th>Step</th>
                                        <th>Error</th>
                                    </tr>
                                </thead>
                                <tbody id="chartRepoTasksFailedBody"></tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
    
    <!-- Error Modal -->
    <div id="errorModal" class="error-modal">
        <div class="error-modal-content">
            <div class="error-modal-header">
                <h3>Error Details</h3>
                <button class="error-modal-close" onclick="closeErrorModal()">&times;</button>
            </div>
            <div class="error-modal-body" id="errorModalBody"></div>
        </div>
    </div>
    
    <script>
        let snapshotData = {};
        let autoRefreshInterval = null;
        
        try {
            snapshotData = JSON.parse('%s');
            // Debug: log app_states to check stage field
            if (snapshotData.app_states) {
                const appKeys = Object.keys(snapshotData.app_states);
                if (appKeys.length > 0) {
                    const firstKey = appKeys[0];
                    const firstApp = snapshotData.app_states[firstKey];
                    console.log('[DEBUG] First app key:', firstKey);
                    console.log('[DEBUG] First app data:', firstApp);
                    console.log('[DEBUG] First app stage:', firstApp ? firstApp.stage : 'undefined', 'type:', typeof (firstApp ? firstApp.stage : 'undefined'));
                }
            }
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
            // Complete debug log for all calls
            const statusType = typeof status;
            const statusIsUndefined = status === undefined;
            const statusIsNull = status === null;
            const statusIsEmpty = status === '';
            const statusIsUnknown = status === 'unknown';
            
            console.log('[DEBUG] getStatusBadge called:', {
                status: status,
                type: statusType,
                isUndefined: statusIsUndefined,
                isNull: statusIsNull,
                isEmpty: statusIsEmpty,
                isUnknown: statusIsUnknown,
                stack: new Error().stack.split('\\n').slice(0, 5).join('\\n')
            });
            
            const statusLower = (status || '').toLowerCase();
            let badgeClass = 'badge-info';
            let badgeText = status || 'unknown';
            
            if (statusLower.includes('running') || statusLower.includes('healthy') || statusLower === 'completed') {
                badgeClass = 'badge-success';
            } else if (statusLower.includes('pending') || statusLower.includes('processing')) {
                badgeClass = 'badge-warning';
            } else if (statusLower.includes('failed') || statusLower.includes('unhealthy') || statusLower.includes('error')) {
                badgeClass = 'badge-danger';
            }
            
            const result = '<span class="badge ' + badgeClass + '">' + badgeText + '</span>';
            console.log('[DEBUG] getStatusBadge returning:', result);
            
            return result;
        }
        
        function renderMarketStats() {
            const summary = snapshotData.summary || {};
            const statsGrid = document.getElementById('marketStatsGrid');
            statsGrid.innerHTML = 
                '<div class="stat-card">' +
                    '<div class="label">Installed Apps</div>' +
                    '<div class="value">' + (summary.total_apps || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Total Tasks <span style="font-size: 10px; color: #666;">(Recent 100)</span></div>' +
                    '<div class="value">' + (summary.total_tasks || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Completed Tasks</div>' +
                    '<div class="value">' + (summary.completed_tasks || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Failed Tasks</div>' +
                    '<div class="value">' + (summary.failed_tasks || 0) + '</div>' +
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
            const appList = Object.values(apps);
            
            // Debug: log all apps with complete information
            console.log('[DEBUG] ========== renderApps START ==========');
            console.log('[DEBUG] Total apps:', appList.length);
            appList.forEach((app, index) => {
                console.log('[DEBUG] App ' + index + ':', {
                    app_name: app.app_name,
                    stage: app.stage,
                    stageType: typeof app.stage,
                    health: app.health,
                    healthType: typeof app.health,
                    user_id: app.user_id,
                    source_id: app.source_id,
                    version: app.version,
                    fullApp: app
                });
            });
            
            // Filter apps by stage
            const appsAll = appList;
            const appsFetching = appList.filter(app => (app.stage || '').toLowerCase() === 'fetching');
            const appsInstalling = appList.filter(app => (app.stage || '').toLowerCase() === 'installing');
            const appsRunning = appList.filter(app => (app.stage || '').toLowerCase() === 'running');
            const appsUpgrading = appList.filter(app => (app.stage || '').toLowerCase() === 'upgrading');
            const appsFailed = appList.filter(app => (app.stage || '').toLowerCase() === 'failed');
            
            // Debug: log filtered results
            console.log('[DEBUG] Filtered apps:', {
                all: appsAll.length,
                fetching: appsFetching.length,
                installing: appsInstalling.length,
                running: appsRunning.length,
                upgrading: appsUpgrading.length,
                failed: appsFailed.length
            });
            
            renderAppsTable('appsTableBody', appsAll);
            renderAppsTable('appsFetchingBody', appsFetching);
            renderAppsTable('appsInstallingBody', appsInstalling);
            renderAppsTable('appsRunningBody', appsRunning);
            renderAppsTable('appsUpgradingBody', appsUpgrading);
            renderAppsTable('appsFailedBody', appsFailed);
            
            console.log('[DEBUG] ========== renderApps END ==========');
        }
        
        function renderAppsTable(tbodyId, apps) {
            console.log('[DEBUG] ========== renderAppsTable START:', tbodyId, '==========');
            
            const tbody = document.getElementById(tbodyId);
            if (!tbody) {
                console.log('[DEBUG] ERROR: renderAppsTable: tbody not found for', tbodyId);
                return;
            }
            tbody.innerHTML = '';
            
            if (apps.length === 0) {
                console.log('[DEBUG] renderAppsTable:', tbodyId, 'has no apps, showing empty state');
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No applications</td></tr>';
                console.log('[DEBUG] ========== renderAppsTable END:', tbodyId, '(empty) ==========');
                return;
            }
            
            console.log('[DEBUG] renderAppsTable:', tbodyId, 'rendering', apps.length, 'apps');
            
            apps.forEach((app, index) => {
                const stageValue = app.stage;
                const healthValue = app.health;
                
                // Complete debug log for each app
                console.log('[DEBUG] renderAppsTable:', tbodyId, 'App', index + '/' + apps.length, ':', {
                    app_name: app.app_name,
                    user_id: app.user_id,
                    source_id: app.source_id,
                    stage: stageValue,
                    stageType: typeof stageValue,
                    stageIsUndefined: stageValue === undefined,
                    stageIsNull: stageValue === null,
                    stageIsEmpty: stageValue === '',
                    health: healthValue,
                    healthType: typeof healthValue,
                    healthIsUndefined: healthValue === undefined,
                    healthIsNull: healthValue === null,
                    healthIsEmpty: healthValue === '',
                    version: app.version,
                    fullAppObject: app
                });
                
                // Call getStatusBadge for stage
                const stageForBadge = stageValue || 'unknown';
                console.log('[DEBUG] Calling getStatusBadge for STAGE:', stageForBadge, 'from app:', app.app_name);
                const badgeHTML = getStatusBadge(stageForBadge);
                
                // Call getStatusBadge for health
                const healthForBadge = healthValue || 'unknown';
                console.log('[DEBUG] Calling getStatusBadge for HEALTH:', healthForBadge, 'from app:', app.app_name);
                const healthBadgeHTML = getStatusBadge(healthForBadge);
                
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + badgeHTML + '</td>' +
                    '<td>' + healthBadgeHTML + '</td>' +
                    '<td>' + (app.version || 'N/A') + '</td>';
                tbody.appendChild(row);
            });
            
            console.log('[DEBUG] ========== renderAppsTable END:', tbodyId, '==========');
        }
        
        function renderTasks() {
            const tasks = snapshotData.tasks || {};
            const taskList = Object.values(tasks);
            
            // Sort tasks by Created At or Updated At (descending)
            taskList.sort((a, b) => {
                const timeA = a.completed_at || a.started_at || a.created_at;
                const timeB = b.completed_at || b.started_at || b.created_at;
                if (!timeA && !timeB) return 0;
                if (!timeA) return 1;
                if (!timeB) return -1;
                return new Date(timeB) - new Date(timeA);
            });
            
            // Filter tasks by status
            const tasksAll = taskList;
            const tasksPending = taskList.filter(task => (task.status || '').toLowerCase() === 'pending');
            const tasksRunning = taskList.filter(task => (task.status || '').toLowerCase() === 'running');
            const tasksCompleted = taskList.filter(task => (task.status || '').toLowerCase() === 'completed');
            const tasksFailed = taskList.filter(task => (task.status || '').toLowerCase() === 'failed');
            
            renderTasksTableFull('tasksTableBody', tasksAll);
            renderTasksTableSimple('tasksPendingBody', tasksPending, 'created_at');
            renderTasksTableSimple('tasksRunningBody', tasksRunning, 'started_at');
            renderTasksTableCompleted('tasksCompletedBody', tasksCompleted);
            renderTasksTableFailed('tasksFailedBody', tasksFailed);
        }
        
        function renderTasksTableFull(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="7" class="empty-state">No tasks</td></tr>';
                return;
            }
            
            tasks.forEach(task => {
                // Determine updated time: use CompletedAt if available, otherwise StartedAt, otherwise CreatedAt
                let updatedTime = task.created_at;
                if (task.completed_at) {
                    updatedTime = task.completed_at;
                } else if (task.started_at) {
                    updatedTime = task.started_at;
                }
                
                // Show error message for failed tasks
                const errorMsg = (task.status === 'failed' && task.error_msg) ? task.error_msg : '-';
                
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.type || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td style="font-size: 11px;">' + formatTimestamp(task.created_at) + '</td>' +
                    '<td style="font-size: 11px;">' + formatTimestamp(updatedTime) + '</td>' +
                    '<td style="font-size: 11px; color: ' + (task.status === 'failed' ? '#991b1b' : '#666') + ';">' + errorMsg + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableSimple(tbodyId, tasks, timeField) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No tasks</td></tr>';
                return;
            }
            
            tasks.forEach(task => {
                let timeValue = task.created_at;
                if (timeField === 'started_at' && task.started_at) {
                    timeValue = task.started_at;
                } else if (timeField === 'completed_at' && task.completed_at) {
                    timeValue = task.completed_at;
                }
                
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.type || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td style="font-size: 11px;">' + formatTimestamp(timeValue) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableCompleted(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No tasks</td></tr>';
                return;
            }
            
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.type || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td style="font-size: 11px;">' + formatTimestamp(task.completed_at || task.started_at || task.created_at) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableFailed(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No tasks</td></tr>';
                return;
            }
            
            tasks.forEach(task => {
                const failedTime = task.completed_at || task.started_at || task.created_at;
                const errorMsg = task.error_msg || '-';
                
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.type || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td style="font-size: 11px; color: #991b1b;">' + errorMsg + '</td>' +
                    '<td style="font-size: 11px;">' + formatTimestamp(failedTime) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderComponents() {
            const components = snapshotData.components || {};
            const tbody = document.getElementById('componentsTableBody');
            tbody.innerHTML = '';
            
            // Filter out syncer, hydrator, and cache as they have separate sections
            const compList = Object.entries(components).filter(([name]) => 
                name !== 'syncer' && name !== 'hydrator' && name !== 'cache'
            );
            
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
        
        function renderSyncer() {
            const components = snapshotData.components || {};
            const syncer = components.syncer;
            const tbody = document.getElementById('syncerTableBody');
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (!syncer) {
                tbody.innerHTML = '<tr><td colspan="2" class="empty-state">No syncer data</td></tr>';
                return;
            }
            
            const metrics = syncer.metrics || {};
            
            // Format duration
            function formatDuration(durationStr) {
                if (!durationStr || durationStr === '0s') return 'N/A';
                return durationStr;
            }
            
            // Format percentage
            function formatPercentage(value) {
                if (value === undefined || value === null) return 'N/A';
                return value.toFixed(2) + '%';
            }
            
            // Calculate progress if current step is available
            let progressInfo = 'N/A';
            if (metrics.current_step && metrics.total_steps > 0 && metrics.current_step_index >= 0) {
                const progress = ((metrics.current_step_index + 1) / metrics.total_steps * 100).toFixed(1);
                progressInfo = (metrics.current_step_index + 1) + '/' + metrics.total_steps + ' (' + progress + '%)';
            }
            
            const stats = [
                { label: 'Status', value: getStatusBadge(syncer.status || 'unknown'), isHtml: true },
                { label: 'Healthy', value: syncer.healthy ? getStatusBadge('healthy') : getStatusBadge('unhealthy'), isHtml: true },
                { label: 'Is Running', value: metrics.is_running ? 'Yes' : 'No' },
                { label: 'Sync Interval', value: metrics.sync_interval || 'N/A' },
                { label: 'Current Step', value: metrics.current_step || 'N/A' },
                { label: 'Progress', value: progressInfo },
                { label: 'Current Source', value: metrics.current_source || 'N/A' },
                { label: 'Last Sync Time', value: metrics.last_sync_time ? formatTimestamp(new Date(metrics.last_sync_time)) : 'Never' },
                { label: 'Last Sync Success', value: metrics.last_sync_success ? formatTimestamp(new Date(metrics.last_sync_success)) : 'Never' },
                { label: 'Last Sync Duration', value: formatDuration(metrics.last_sync_duration) },
                { label: 'Last Synced Apps (Source: ' + (metrics.current_source || 'N/A') + ')', value: metrics.last_synced_app_count !== undefined ? metrics.last_synced_app_count : 'N/A' },
                { label: 'Next Sync Time', value: metrics.next_sync_time ? formatTimestamp(new Date(metrics.next_sync_time)) : 'N/A' },
                { label: 'Total App Detail Syncs', value: metrics.total_syncs !== undefined ? metrics.total_syncs : 0 },
                { label: 'Success Count (App Detail Syncs)', value: metrics.success_count !== undefined ? metrics.success_count : 0 },
                { label: 'Failure Count (App Detail Syncs)', value: metrics.failure_count !== undefined ? metrics.failure_count : 0 },
                { label: 'Consecutive Failures', value: metrics.consecutive_failures !== undefined ? metrics.consecutive_failures : 0 },
                { label: 'Success Rate', value: formatPercentage(metrics.success_rate) },
                { label: 'Step Count', value: metrics.step_count || 0 },
                { label: 'Steps', value: Array.isArray(metrics.steps) ? metrics.steps.join(', ') : 'N/A' },
                { label: 'Last Sync Error', value: metrics.last_sync_error || 'None', style: metrics.last_sync_error ? 'color: #dc2626;' : '' },
                { label: 'Last Check', value: formatTimestamp(syncer.last_check) },
            ];
            
            stats.forEach(stat => {
                const row = document.createElement('tr');
                const valueCell = stat.style ? 
                    '<td style="' + stat.style + '">' + (stat.isHtml ? stat.value : stat.value) + '</td>' :
                    '<td>' + (stat.isHtml ? stat.value : stat.value) + '</td>';
                row.innerHTML = '<td><strong>' + stat.label + '</strong></td>' + valueCell;
                tbody.appendChild(row);
            });
            
            // Add last sync details if available
            if (metrics.last_sync_details) {
                const details = metrics.last_sync_details;
                const detailsRow = document.createElement('tr');
                detailsRow.innerHTML = '<td colspan="2" style="padding-top: 16px;"><strong>Last Sync Details</strong></td>';
                tbody.appendChild(detailsRow);
                
                // Add sync time
                if (details.sync_time) {
                    const row = document.createElement('tr');
                    row.innerHTML = '<td style="padding-left: 20px;">Sync Time</td><td>' + formatTimestamp(new Date(details.sync_time)) + '</td>';
                    tbody.appendChild(row);
                }
                
                // Add succeeded apps
                if (details.succeeded_apps && details.succeeded_apps.length > 0) {
                    const row = document.createElement('tr');
                    row.innerHTML = '<td style="padding-left: 20px;">Succeeded Apps (' + details.succeeded_apps.length + ')</td><td>' + details.succeeded_apps.join(', ') + '</td>';
                    tbody.appendChild(row);
                }
                
                // Add failed apps
                if (details.failed_apps && details.failed_apps.length > 0) {
                    const row = document.createElement('tr');
                    let failedAppsHtml = '<div style="max-height: 200px; overflow-y: auto;">';
                    details.failed_apps.forEach((failedApp, index) => {
                        failedAppsHtml += '<div style="margin-bottom: 8px;">';
                        failedAppsHtml += '<strong>' + (failedApp.app_name || failedApp.app_id) + '</strong>: ';
                        failedAppsHtml += '<span style="color: #dc2626;">' + (failedApp.reason || 'Unknown error') + '</span>';
                        failedAppsHtml += '</div>';
                    });
                    failedAppsHtml += '</div>';
                    row.innerHTML = '<td style="padding-left: 20px;">Failed Apps (' + details.failed_apps.length + ')</td><td>' + failedAppsHtml + '</td>';
                    tbody.appendChild(row);
                }
            }
            
            // Add status message if available
            if (syncer.message) {
                const row = document.createElement('tr');
                row.innerHTML = '<td colspan="2" style="color: #dc2626; padding-top: 8px;"><strong>Note:</strong> ' + syncer.message + '</td>';
                tbody.appendChild(row);
            }
        }
        
        function renderHydrator() {
            const components = snapshotData.components || {};
            const hydrator = components.hydrator;
            const tbody = document.getElementById('hydratorTableBody');
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (!hydrator) {
                tbody.innerHTML = '<tr><td colspan="2" class="empty-state">No hydrator data</td></tr>';
                return;
            }
            
            const metrics = hydrator.metrics || {};
            
            // Calculate success rate
            let successRate = 'N/A';
            if (metrics.total_tasks_processed > 0) {
                successRate = ((metrics.total_tasks_succeeded / metrics.total_tasks_processed) * 100).toFixed(2) + '%';
            }
            
            // Calculate failure rate
            let failureRate = 'N/A';
            if (metrics.total_tasks_processed > 0) {
                failureRate = ((metrics.total_tasks_failed / metrics.total_tasks_processed) * 100).toFixed(2) + '%';
            }
            
            const stats = [
                { label: 'Status', value: getStatusBadge(hydrator.status || 'unknown'), isHtml: true },
                { label: 'Healthy', value: hydrator.healthy ? getStatusBadge('healthy') : getStatusBadge('unhealthy'), isHtml: true },
                { label: 'Is Running', value: metrics.is_running ? 'Yes' : 'No' },
                { label: 'Enabled', value: metrics.enabled !== undefined ? (metrics.enabled ? 'Yes' : 'No') : 'N/A' },
                { label: 'Queue Length', value: metrics.queue_length !== undefined ? metrics.queue_length : 0 },
                { label: 'Active Tasks', value: metrics.active_tasks_count !== undefined ? metrics.active_tasks_count : 0 },
                { label: 'Total Processed', value: metrics.total_tasks_processed !== undefined ? metrics.total_tasks_processed : 0 },
                { label: 'Total Succeeded', value: metrics.total_tasks_succeeded !== undefined ? metrics.total_tasks_succeeded : 0 },
                { label: 'Total Failed', value: metrics.total_tasks_failed !== undefined ? metrics.total_tasks_failed : 0 },
                { label: 'Success Rate', value: successRate },
                { label: 'Failure Rate', value: failureRate },
                { label: 'Completed Tasks (Recent)', value: metrics.completed_tasks_count !== undefined ? metrics.completed_tasks_count : 0 },
                { label: 'Failed Tasks', value: metrics.failed_tasks_count !== undefined ? metrics.failed_tasks_count : 0 },
                { label: 'Last Check', value: formatTimestamp(hydrator.last_check) },
            ];
            
            stats.forEach(stat => {
                const row = document.createElement('tr');
                row.innerHTML = '<td><strong>' + stat.label + '</strong></td>' +
                    '<td>' + (stat.isHtml ? stat.value : stat.value) + '</td>';
                tbody.appendChild(row);
            });
            
            // Add status message if available
            if (hydrator.message) {
                const row = document.createElement('tr');
                row.innerHTML = '<td colspan="2" style="color: #dc2626; padding-top: 8px;"><strong>Note:</strong> ' + hydrator.message + '</td>';
                tbody.appendChild(row);
            }
            
            // Render active tasks
            renderHydratorActiveTasks(metrics.active_tasks);
            
            // Render worker status
            renderHydratorWorkers(metrics.workers);
            
            // Render task history
            renderHydratorHistory(metrics.recent_completed_tasks, metrics.recent_failed_tasks);
        }
        
        function renderHydratorActiveTasks(activeTasks) {
            const container = document.getElementById('hydratorActiveTasks');
            if (!container) return;
            
            if (!activeTasks || activeTasks.length === 0) {
                container.innerHTML = '<h3>Active Tasks</h3><p class="empty-state">No active tasks</p>';
                return;
            }
            
            let html = '<h3>Active Tasks (' + activeTasks.length + ')</h3>';
            html += '<div class="table-container" style="max-height: 300px; overflow-y: auto;">';
            html += '<table style="font-size: 12px;"><thead><tr>';
            html += '<th>Task ID</th><th>App ID</th><th>App Name</th><th>User ID</th><th>Source ID</th>';
            html += '<th>Current Step</th><th>Status</th><th>Started At</th>';
            html += '</tr></thead><tbody>';
            
            activeTasks.forEach(task => {
                const stepInfo = task.current_step ? task.current_step + ' (' + (task.step_index + 1) + '/' + task.total_steps + ')' : 'N/A';
                html += '<tr>';
                html += '<td>' + (task.task_id || 'N/A').substring(0, 20) + '...' + '</td>';
                html += '<td>' + (task.app_id || 'N/A') + '</td>';
                html += '<td>' + (task.app_name || 'N/A') + '</td>';
                html += '<td>' + (task.user_id || 'N/A') + '</td>';
                html += '<td>' + (task.source_id || 'N/A') + '</td>';
                html += '<td>' + stepInfo + '</td>';
                html += '<td>' + getStatusBadge(task.status || 'running') + '</td>';
                html += '<td>' + (task.started_at ? formatTimestamp(new Date(task.started_at)) : 'N/A') + '</td>';
                html += '</tr>';
            });
            
            html += '</tbody></table></div>';
            container.innerHTML = html;
        }
        
        function renderHydratorWorkers(workers) {
            const container = document.getElementById('hydratorWorkers');
            if (!container) return;
            
            if (!workers || workers.length === 0) {
                container.innerHTML = '<h3>Workers</h3><p class="empty-state">No worker information</p>';
                return;
            }
            
            let html = '<h3>Workers (' + workers.length + ')</h3>';
            html += '<div class="table-container" style="max-height: 300px; overflow-y: auto;">';
            html += '<table style="font-size: 12px;"><thead><tr>';
            html += '<th>Worker ID</th><th>Status</th><th>Current Task</th><th>App ID</th><th>Step</th><th>Progress</th><th>Last Activity</th>';
            html += '</tr></thead><tbody>';
            
            workers.forEach(worker => {
                const statusBadge = worker.is_idle ? getStatusBadge('idle') : getStatusBadge('busy');
                const task = worker.current_task;
                html += '<tr>';
                html += '<td>' + worker.worker_id + '</td>';
                html += '<td>' + statusBadge + '</td>';
                if (task) {
                    const progress = task.progress ? task.progress.toFixed(1) + '%' : 'N/A';
                    html += '<td>' + (task.task_id || 'N/A').substring(0, 15) + '...' + '</td>';
                    html += '<td>' + (task.app_id || 'N/A') + '</td>';
                    html += '<td>' + (task.current_step || 'N/A') + '</td>';
                    html += '<td>' + progress + '</td>';
                } else {
                    html += '<td colspan="4" class="empty-state">Idle</td>';
                }
                html += '<td>' + (worker.last_activity ? formatTimestamp(new Date(worker.last_activity)) : 'N/A') + '</td>';
                html += '</tr>';
            });
            
            html += '</tbody></table></div>';
            container.innerHTML = html;
        }
        
        function renderHydratorHistory(completedTasks, failedTasks) {
            const container = document.getElementById('hydratorHistory');
            if (!container) return;
            
            const hasCompleted = completedTasks && completedTasks.length > 0;
            const hasFailed = failedTasks && failedTasks.length > 0;
            
            if (!hasCompleted && !hasFailed) {
                container.innerHTML = '<h3>Task History</h3><p class="empty-state">No task history</p>';
                return;
            }
            
            let html = '<h3>Task History (Recent 50 Tasks)</h3>';
            
            // Recent completed tasks
            if (hasCompleted) {
                html += '<h4 style="margin-top: 16px; color: #059669;">Recent Completed Tasks (' + completedTasks.length + ')</h4>';
                html += '<div class="table-container" style="max-height: 200px; overflow-y: auto; margin-bottom: 20px;">';
                html += '<table style="font-size: 12px;"><thead><tr>';
                html += '<th>Task ID</th><th>App ID</th><th>App Name</th><th>User ID</th><th>Duration</th><th>Completed At</th>';
                html += '</tr></thead><tbody>';
                
                completedTasks.slice(0, 10).forEach(task => {
                    const duration = task.duration ? formatDuration(task.duration) : 'N/A';
                    html += '<tr>';
                    html += '<td>' + (task.task_id || 'N/A').substring(0, 20) + '...' + '</td>';
                    html += '<td>' + (task.app_id || 'N/A') + '</td>';
                    html += '<td>' + (task.app_name || 'N/A') + '</td>';
                    html += '<td>' + (task.user_id || 'N/A') + '</td>';
                    html += '<td>' + duration + '</td>';
                    html += '<td>' + (task.completed_at ? formatTimestamp(new Date(task.completed_at)) : 'N/A') + '</td>';
                    html += '</tr>';
                });
                
                html += '</tbody></table></div>';
            }
            
            // Recent failed tasks
            if (hasFailed) {
                html += '<h4 style="margin-top: 16px; color: #dc2626;">Recent Failed Tasks (' + failedTasks.length + ')</h4>';
                html += '<div class="table-container" style="max-height: 200px; overflow-y: auto;">';
                html += '<table style="font-size: 12px;"><thead><tr>';
                html += '<th>Task ID</th><th>App ID</th><th>App Name</th><th>User ID</th><th>Failed Step</th><th>Error</th><th>Failed At</th>';
                html += '</tr></thead><tbody>';
                
                failedTasks.slice(0, 10).forEach(task => {
                    const errorMsg = task.error_msg || 'N/A';
                    const errorId = 'error-' + (task.task_id || Math.random().toString(36).substr(2, 9));
                    html += '<tr>';
                    html += '<td>' + (task.task_id || 'N/A').substring(0, 20) + '...' + '</td>';
                    html += '<td>' + (task.app_id || 'N/A') + '</td>';
                    html += '<td>' + (task.app_name || 'N/A') + '</td>';
                    html += '<td>' + (task.user_id || 'N/A') + '</td>';
                    html += '<td>' + (task.failed_step || 'N/A') + '</td>';
                    html += '<td style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">';
                    html += '<span id="' + errorId + '-short" style="cursor: pointer; color: #dc2626;" onclick="showErrorModal(\'' + errorId + '\', \'' + errorMsg.replace(/'/g, "\\'").replace(/"/g, '&quot;') + '\')">';
                    html += (errorMsg.length > 50 ? errorMsg.substring(0, 50) + '...' : errorMsg) + '</span>';
                    html += '</td>';
                    html += '<td>' + (task.completed_at ? formatTimestamp(new Date(task.completed_at)) : 'N/A') + '</td>';
                    html += '</tr>';
                });
                
                html += '</tbody></table></div>';
            }
            
            container.innerHTML = html;
        }
        
        function formatDuration(durationStr) {
            if (!durationStr) return 'N/A';
            // Try to parse duration string like "1h30m45s" or number in seconds
            if (typeof durationStr === 'string') {
                return durationStr;
            }
            // If it's a number, assume it's nanoseconds and convert
            if (typeof durationStr === 'number') {
                const seconds = Math.floor(durationStr / 1e9);
                const hours = Math.floor(seconds / 3600);
                const minutes = Math.floor((seconds % 3600) / 60);
                const secs = seconds % 60;
                if (hours > 0) {
                    return hours + 'h' + minutes + 'm' + secs + 's';
                } else if (minutes > 0) {
                    return minutes + 'm' + secs + 's';
                } else {
                    return secs + 's';
                }
            }
            return 'N/A';
        }
        
        function renderCacheStats() {
            const components = snapshotData.components || {};
            const cache = components.cache;
            const tbody = document.getElementById('cacheStatsTableBody');
            tbody.innerHTML = '';
            
            if (!cache || !cache.metrics) {
                tbody.innerHTML = '<tr><td colspan="2" class="empty-state">No cache statistics</td></tr>';
                return;
            }
            
            const metrics = cache.metrics;
            const stats = [
                { label: 'Total Users', key: 'total_users' },
                { label: 'Total Sources', key: 'total_sources' },
                { label: 'Total Apps', key: 'total_apps' },
                { label: 'App Info Latest', key: 'total_app_info_latest' },
                { label: 'App State Latest', key: 'total_app_state_latest' },
                { label: 'App Info Pending', key: 'total_app_info_pending' },
                { label: 'Is Running', key: 'is_running' },
            ];
            
            stats.forEach(stat => {
                const value = metrics[stat.key];
                if (value !== undefined) {
                    const row = document.createElement('tr');
                    row.innerHTML = '<td>' + stat.label + '</td>' +
                        '<td>' + (typeof value === 'boolean' ? (value ? 'Yes' : 'No') : value) + '</td>';
                    tbody.appendChild(row);
                }
            });
        }
        
        function showMainTab(tabName, element) {
            // Find the parent panel to scope the tab switching
            const panel = element ? element.closest('.panel') : null;
            if (!panel) {
                // Fallback: find panel by checking which panel contains this tab
                const tabElement = document.getElementById(tabName);
                if (tabElement) {
                    const parentPanel = tabElement.closest('.panel');
                    if (parentPanel) {
                        // Hide only main tabs within this panel
                        parentPanel.querySelectorAll('.main-tab-content').forEach(content => {
                            content.classList.remove('active');
                        });
                        parentPanel.querySelectorAll('.main-tab').forEach(tab => {
                            tab.classList.remove('active');
                        });
                    }
                }
            } else {
                // Hide only main tabs within this panel
                panel.querySelectorAll('.main-tab-content').forEach(content => {
                    content.classList.remove('active');
                });
                panel.querySelectorAll('.main-tab').forEach(tab => {
                    tab.classList.remove('active');
                });
            }
            
            // Show selected main tab
            const tabContent = document.getElementById(tabName);
            if (tabContent) {
                tabContent.classList.add('active');
            }
            if (element) {
                element.classList.add('active');
            }
            
            // Reset sub-tabs to show first sub-tab by default
            const subTabs = document.querySelectorAll('#' + tabName + ' .sub-tab');
            const subTabContents = document.querySelectorAll('#' + tabName + ' .sub-tab-content');
            subTabs.forEach(tab => tab.classList.remove('active'));
            subTabContents.forEach(content => content.classList.remove('active'));
            if (subTabs.length > 0) {
                subTabs[0].classList.add('active');
            }
            if (subTabContents.length > 0) {
                subTabContents[0].classList.add('active');
            }
            
            // Re-render data for the selected tab
            if (tabName.startsWith('market')) {
                if (tabName === 'marketApps') {
                    renderApps();
                } else if (tabName === 'marketTasks') {
                    renderTasks();
                } else if (tabName === 'marketCache') {
                    renderSyncer();
                    renderHydrator();
                    renderCacheStats();
                }
            } else if (tabName.startsWith('chartRepo')) {
                renderChartRepo();
            }
        }
        
        function showSubTab(mainTabName, subTabName, element) {
            // Hide all sub-tabs in this main tab
            const mainTab = document.getElementById(mainTabName);
            mainTab.querySelectorAll('.sub-tab-content').forEach(content => {
                content.classList.remove('active');
            });
            mainTab.querySelectorAll('.sub-tab').forEach(tab => {
                tab.classList.remove('active');
            });
            
            // Show selected sub-tab
            const subTabContent = document.getElementById(mainTabName + '-' + subTabName);
            if (subTabContent) {
                subTabContent.classList.add('active');
            }
            if (element) {
                element.classList.add('active');
            }
            
            // Re-render data for the selected sub-tab
            if (mainTabName.startsWith('market')) {
                if (mainTabName === 'marketApps') {
                    renderApps();
                } else if (mainTabName === 'marketTasks') {
                    renderTasks();
                } else if (mainTabName === 'marketCache') {
                    if (subTabName === 'syncer') {
                        renderSyncer();
                    } else if (subTabName === 'hydrator') {
                        renderHydrator();
                    } else if (subTabName === 'cache') {
                        renderCacheStats();
                    }
                }
            } else if (mainTabName.startsWith('chartRepo')) {
                if (mainTabName === 'chartRepoImageAnalyzer') {
                    // Re-render Image Analyzer data when switching sub-tabs
                    const chartRepo = snapshotData.chart_repo || {};
                    const imageAnalyzer = chartRepo.tasks && chartRepo.tasks.image_analyzer;
                    if (subTabName === 'overview') {
                        renderImageAnalyzerOverview('chartRepoImageAnalyzerOverviewBody', imageAnalyzer);
                    } else if (subTabName === 'queued') {
                        renderImageAnalyzerTasks('chartRepoImageAnalyzerQueuedBody', imageAnalyzer ? (imageAnalyzer.queued_tasks || []) : []);
                    } else if (subTabName === 'processing') {
                        renderImageAnalyzerTasks('chartRepoImageAnalyzerProcessingBody', imageAnalyzer ? (imageAnalyzer.processing_tasks || []) : []);
                    } else if (subTabName === 'completed') {
                        renderImageAnalyzerTasks('chartRepoImageAnalyzerCompletedBody', imageAnalyzer ? (imageAnalyzer.recent_completed || []) : []);
                    } else if (subTabName === 'failed') {
                        renderImageAnalyzerTasks('chartRepoImageAnalyzerFailedBody', imageAnalyzer ? (imageAnalyzer.recent_failed || []) : []);
                    }
                } else {
                    renderChartRepo();
                }
            }
        }
        
        function renderChartRepo() {
            const chartRepo = snapshotData.chart_repo || {};
            
            // Render Applications
            const apps = chartRepo.apps || [];
            const appsAll = apps;
            const appsProcessing = apps.filter(app => (app.state || '').toLowerCase().includes('processing'));
            const appsCompleted = apps.filter(app => (app.state || '').toLowerCase() === 'completed');
            const appsFailed = apps.filter(app => (app.state || '').toLowerCase() === 'failed' || app.error);
            
            renderAppsTable('chartRepoAppsTableBody', appsAll);
            renderAppsTable('chartRepoAppsProcessingBody', appsProcessing);
            renderAppsTable('chartRepoAppsCompletedBody', appsCompleted);
            renderAppsTableWithError('chartRepoAppsFailedBody', appsFailed);
            
            // Render Image Analyzer - always render all tabs to ensure data is available
            const imageAnalyzer = chartRepo.tasks && chartRepo.tasks.image_analyzer;
            if (imageAnalyzer) {
                renderImageAnalyzerOverview('chartRepoImageAnalyzerOverviewBody', imageAnalyzer);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerQueuedBody', imageAnalyzer.queued_tasks || []);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerProcessingBody', imageAnalyzer.processing_tasks || []);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerCompletedBody', imageAnalyzer.recent_completed || []);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerFailedBody', imageAnalyzer.recent_failed || []);
            } else {
                // Clear all Image Analyzer tabs if no data
                renderImageAnalyzerOverview('chartRepoImageAnalyzerOverviewBody', null);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerQueuedBody', []);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerProcessingBody', []);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerCompletedBody', []);
                renderImageAnalyzerTasks('chartRepoImageAnalyzerFailedBody', []);
            }
            
            // Render Tasks
            const hydrator = chartRepo.tasks && chartRepo.tasks.hydrator;
            const tasks = hydrator ? (hydrator.tasks || []) : [];
            const tasksAll = tasks;
            const tasksPending = tasks.filter(task => (task.status || '').toLowerCase() === 'pending');
            const tasksRunning = tasks.filter(task => (task.status || '').toLowerCase() === 'running');
            const tasksCompleted = tasks.filter(task => (task.status || '').toLowerCase() === 'completed');
            const tasksFailed = tasks.filter(task => (task.status || '').toLowerCase() === 'failed' || task.last_error);
            
            renderTasksTable('chartRepoTasksTableBody', tasksAll);
            renderTasksTableSimple('chartRepoTasksPendingBody', tasksPending);
            renderTasksTable('chartRepoTasksRunningBody', tasksRunning);
            renderTasksTableSimple('chartRepoTasksCompletedBody', tasksCompleted);
            renderTasksTableWithError('chartRepoTasksFailedBody', tasksFailed);
        }
        
        function renderAppsTable(tbodyId, apps) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (apps.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No applications</td></tr>';
                return;
            }
            apps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.state || 'unknown') + '</td>' +
                    '<td>' + (app.current_step ? app.current_step.name : '-') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderAppsTableWithError(tbodyId, apps) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (apps.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No applications</td></tr>';
                return;
            }
            apps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.state || 'unknown') + '</td>' +
                    '<td>' + (app.error ? app.error.message : '-') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderImagesTable(tbodyId, images) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (images.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No images</td></tr>';
                return;
            }
            images.forEach(image => {
                const progress = image.download_progress || 0;
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (image.image_name || 'N/A') + '</td>' +
                    '<td>' + (image.app_name || '-') + '</td>' +
                    '<td>' + getStatusBadge(image.status || 'unknown') + '</td>' +
                    '<td><div class="progress-bar"><div class="progress-fill" style="width: ' + progress.toFixed(2) + '%"></div></div></td>';
                tbody.appendChild(row);
            });
        }
        
        function renderImagesTableSimple(tbodyId, images) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (images.length === 0) {
                tbody.innerHTML = '<tr><td colspan="3" class="empty-state">No images</td></tr>';
                return;
            }
            images.forEach(image => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (image.image_name || 'N/A') + '</td>' +
                    '<td>' + (image.app_name || '-') + '</td>' +
                    '<td>' + getStatusBadge(image.status || 'unknown') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderImagesTableWithSize(tbodyId, images) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (images.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No images</td></tr>';
                return;
            }
            images.forEach(image => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (image.image_name || 'N/A') + '</td>' +
                    '<td>' + (image.app_name || '-') + '</td>' +
                    '<td>' + getStatusBadge(image.status || 'unknown') + '</td>' +
                    '<td>' + formatBytes(image.total_size) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderImagesTableWithError(tbodyId, images) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (images.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No images</td></tr>';
                return;
            }
            images.forEach(image => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (image.image_name || 'N/A') + '</td>' +
                    '<td>' + (image.app_name || '-') + '</td>' +
                    '<td>' + getStatusBadge(image.status || 'unknown') + '</td>' +
                    '<td style="font-size: 11px;">' + (image.error_message || '-') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderImageAnalyzerOverview(tbodyId, imageAnalyzer) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (!imageAnalyzer) {
                tbody.innerHTML = '<tr><td colspan="2" class="empty-state">No Image Analyzer data</td></tr>';
                return;
            }
            
            const formatDuration = (ms) => {
                if (!ms) return 'N/A';
                const seconds = Math.floor(ms / 1000);
                if (seconds < 60) return seconds + 's';
                const minutes = Math.floor(seconds / 60);
                const secs = seconds % 60;
                return minutes + 'm ' + secs + 's';
            };
            
            const stats = [
                { label: 'Is Running', value: imageAnalyzer.is_running ? 'Yes' : 'No' },
                { label: 'Health Status', value: getStatusBadge(imageAnalyzer.health_status || 'unknown'), isHtml: true },
                { label: 'Last Check', value: formatTimestamp(imageAnalyzer.last_check) },
                { label: 'Queue Length', value: imageAnalyzer.queue_length || 0 },
                { label: 'Active Workers', value: imageAnalyzer.active_workers || 0 },
                { label: 'Cached Images', value: imageAnalyzer.cached_images || 0 },
                { label: 'Analyzing Count', value: imageAnalyzer.analyzing_count || 0 },
                { label: 'Total Analyzed', value: imageAnalyzer.total_analyzed || 0 },
                { label: 'Successful Analyzed', value: imageAnalyzer.successful_analyzed || 0 },
                { label: 'Failed Analyzed', value: imageAnalyzer.failed_analyzed || 0 },
                { label: 'Average Analysis Time', value: imageAnalyzer.average_analysis_time ? formatDuration(imageAnalyzer.average_analysis_time) : 'N/A' },
                { label: 'Queued Tasks', value: (imageAnalyzer.queued_tasks || []).length },
                { label: 'Processing Tasks', value: (imageAnalyzer.processing_tasks || []).length },
                { label: 'Recent Completed', value: (imageAnalyzer.recent_completed || []).length },
                { label: 'Recent Failed', value: (imageAnalyzer.recent_failed || []).length },
            ];
            
            if (imageAnalyzer.error_message) {
                stats.push({ label: 'Error Message', value: imageAnalyzer.error_message });
            }
            
            stats.forEach(stat => {
                const row = document.createElement('tr');
                row.innerHTML = '<td><strong>' + stat.label + '</strong></td>' +
                    '<td>' + (stat.isHtml ? stat.value : stat.value) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderImageAnalyzerTasks(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No tasks</td></tr>';
                return;
            }
            
            tasks.forEach(task => {
                const row = document.createElement('tr');
                let cells = '';
                
                // Common cells
                cells += '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>';
                cells += '<td>' + (task.app_name || 'N/A') + '</td>';
                cells += '<td>' + getStatusBadge(task.status || 'unknown') + '</td>';
                
                // Context-specific cells based on tbodyId
                if (tbodyId.includes('Queued')) {
                    cells += '<td>' + (task.images_count || 0) + '</td>';
                    const createdAt = task.created_at;
                    if (createdAt) {
                        const createdAtDate = typeof createdAt === 'string' ? new Date(createdAt) : createdAt;
                        cells += '<td style="font-size: 11px;">' + formatTimestamp(createdAtDate) + '</td>';
                    } else {
                        cells += '<td style="font-size: 11px;">-</td>';
                    }
                } else if (tbodyId.includes('Processing')) {
                    cells += '<td>' + (task.worker_id !== undefined && task.worker_id !== null ? task.worker_id : '-') + '</td>';
                    const progress = task.images_count > 0 ? ((task.analyzed_count || 0) / task.images_count * 100).toFixed(1) : '0';
                    cells += '<td>' + (task.analyzed_count || 0) + '/' + (task.images_count || 0) + ' (' + progress + '%)</td>';
                    const startedAt = task.started_at;
                    if (startedAt) {
                        const startedAtDate = typeof startedAt === 'string' ? new Date(startedAt) : startedAt;
                        cells += '<td style="font-size: 11px;">' + formatTimestamp(startedAtDate) + '</td>';
                    } else {
                        cells += '<td style="font-size: 11px;">-</td>';
                    }
                } else if (tbodyId.includes('Completed')) {
                    let duration = '-';
                    if (task.duration) {
                        if (typeof task.duration === 'string') {
                            duration = task.duration;
                        } else if (typeof task.duration === 'number') {
                            // Assume nanoseconds
                            duration = (task.duration / 1000000000).toFixed(2) + 's';
                        }
                    }
                    cells += '<td>' + duration + '</td>';
                    const completedAt = task.completed_at;
                    if (completedAt) {
                        const completedAtDate = typeof completedAt === 'string' ? new Date(completedAt) : completedAt;
                        cells += '<td style="font-size: 11px;">' + formatTimestamp(completedAtDate) + '</td>';
                    } else {
                        cells += '<td style="font-size: 11px;">-</td>';
                    }
                } else if (tbodyId.includes('Failed')) {
                    cells += '<td style="font-size: 11px; color: #991b1b;">' + (task.error || '-') + '</td>';
                    cells += '<td>' + (task.error_step || '-') + '</td>';
                    let failedAt = null;
                    if (task.completed_at) {
                        failedAt = typeof task.completed_at === 'string' ? new Date(task.completed_at) : task.completed_at;
                    } else if (task.started_at) {
                        failedAt = typeof task.started_at === 'string' ? new Date(task.started_at) : task.started_at;
                    } else if (task.created_at) {
                        failedAt = typeof task.created_at === 'string' ? new Date(task.created_at) : task.created_at;
                    }
                    cells += '<td style="font-size: 11px;">' + (failedAt ? formatTimestamp(failedAt) : '-') + '</td>';
                }
                
                row.innerHTML = cells;
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTable(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No tasks</td></tr>';
                return;
            }
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.step_name || '-') + '</td>' +
                    '<td>' + (task.retry_count || 0) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableSimple(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No tasks</td></tr>';
                return;
            }
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.step_name || '-') + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableWithError(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No tasks</td></tr>';
                return;
            }
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.step_name || '-') + '</td>' +
                    '<td style="font-size: 11px;">' + (task.last_error || '-') + '</td>';
                tbody.appendChild(row);
            });
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
            renderSyncer();
            renderHydrator();
            renderCacheStats();
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
        
        function showErrorModal(errorId, errorMsg) {
            const modal = document.getElementById('errorModal');
            const modalBody = document.getElementById('errorModalBody');
            modalBody.textContent = errorMsg;
            modal.style.display = 'block';
        }
        
        function closeErrorModal() {
            const modal = document.getElementById('errorModal');
            modal.style.display = 'none';
        }
        
        // Close modal when clicking outside
        window.onclick = function(event) {
            const modal = document.getElementById('errorModal');
            if (event.target == modal) {
                closeErrorModal();
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
