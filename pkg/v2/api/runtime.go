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
                    <div class="table-container">
                        <table id="appsTable">
                            <thead>
                                <tr>
                                    <th>App Name</th>
                                    <th>User ID</th>
                                    <th>Source ID</th>
                                    <th>Version</th>
                                </tr>
                            </thead>
                            <tbody id="appsTableBody"></tbody>
                        </table>
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
                                        <th>Updated At</th>
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
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
                                        <th>Updated At</th>
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
            const imageAnalyzer = chartRepo.tasks && chartRepo.tasks.image_analyzer;
            const hydratorTasks = hydrator ? (hydrator.tasks || []) : [];
            const hydratorPendingCount = hydratorTasks.filter(task => (task.status || '').toLowerCase() === 'pending').length;
            const hydratorRunningCount = hydratorTasks.filter(task => (task.status || '').toLowerCase() === 'running').length;
            const hydratorQueueLength = hydrator
                ? (hydrator.queue_length !== undefined ? hydrator.queue_length : (hydratorPendingCount + hydratorRunningCount))
                : 0;
            const hydratorActiveTasks = hydrator
                ? (hydrator.active_tasks !== undefined ? hydrator.active_tasks : hydratorRunningCount)
                : 0;
            const statsGrid = document.getElementById('chartRepoStatsGrid');
            statsGrid.innerHTML = 
                '<div class="stat-card">' +
                    '<div class="label">Total Apps</div>' +
                    '<div class="value">' + ((chartRepo.apps && chartRepo.apps.length) || 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Total Images</div>' +
                    '<div class="value">' + (imageAnalyzer ? (imageAnalyzer.total_analyzed || 0) : 0) + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Active Tasks</div>' +
                    '<div class="value">' + hydratorActiveTasks + '</div>' +
                '</div>' +
                '<div class="stat-card">' +
                    '<div class="label">Queue Length</div>' +
                    '<div class="value">' + hydratorQueueLength + '</div>' +
                '</div>';
        }
        
        function renderApps() {
            const apps = snapshotData.app_states || {};
            const appList = Object.values(apps);
            
            renderMarketAppsTable('appsTableBody', appList);
        }
        
        function renderMarketAppsTable(tbodyId, apps) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            
            if (apps.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No applications</td></tr>';
                return;
            }
            
            apps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + (app.version || 'N/A') + '</td>';
                tbody.appendChild(row);
            });
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
            
            // Ensure we have arrays, not undefined/null
            const completed = Array.isArray(completedTasks) ? completedTasks : [];
            const failed = Array.isArray(failedTasks) ? failedTasks : [];
            
            const hasCompleted = completed.length > 0;
            const hasFailed = failed.length > 0;
            
            if (!hasCompleted && !hasFailed) {
                container.innerHTML = '<h3>Task History</h3><p class="empty-state">No task history</p>';
                return;
            }
            
            let html = '<h3>Task History (Recent 50 Tasks)</h3>';
            
            // Recent completed tasks
            if (hasCompleted) {
                html += '<h4 style="margin-top: 16px; color: #059669;">Recent Completed Tasks (' + completed.length + ')</h4>';
                html += '<div class="table-container" style="max-height: 200px; overflow-y: auto; margin-bottom: 20px;">';
                html += '<table style="font-size: 12px;"><thead><tr>';
                html += '<th>Task ID</th><th>App ID</th><th>App Name</th><th>User ID</th><th>Duration</th><th>Completed At</th>';
                html += '</tr></thead><tbody>';
                
                completed.slice(0, 10).forEach(task => {
                    const duration = task.duration ? formatDuration(task.duration) : 'N/A';
                    const taskId = task.task_id || 'N/A';
                    const taskIdDisplay = taskId.length > 20 ? taskId.substring(0, 20) + '...' : taskId;
                    html += '<tr>';
                    html += '<td>' + taskIdDisplay + '</td>';
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
                html += '<h4 style="margin-top: 16px; color: #dc2626;">Recent Failed Tasks (' + failed.length + ')</h4>';
                html += '<div class="table-container" style="max-height: 200px; overflow-y: auto;">';
                html += '<table style="font-size: 12px;"><thead><tr>';
                html += '<th>Task ID</th><th>App ID</th><th>App Name</th><th>User ID</th><th>Failed Step</th><th>Error</th><th>Failed At</th>';
                html += '</tr></thead><tbody>';
                
                failed.slice(0, 10).forEach(task => {
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
            
            // Deduplicate apps by user_id:source_id:app_id (keep the one with latest updated_at)
            const appMap = new Map();
            apps.forEach(app => {
                const appKey = (app.user_id || '') + ':' + (app.source_id || '') + ':' + (app.app_id || app.app_name || '');
                if (!appKey || appKey === '::') return;
                
                const existing = appMap.get(appKey);
                if (!existing) {
                    appMap.set(appKey, app);
                } else {
                    // Keep the one with latest updated_at
                    const existingTime = getChartRepoAppUpdatedAtValue(existing);
                    const currentTime = getChartRepoAppUpdatedAtValue(app);
                    if (currentTime > existingTime) {
                        appMap.set(appKey, app);
                    }
                }
            });
            const uniqueApps = Array.from(appMap.values());
            
            const sortedApps = [...uniqueApps].sort((a, b) => getChartRepoAppUpdatedAtValue(b) - getChartRepoAppUpdatedAtValue(a));
            const appsProcessing = sortedApps.filter(app => (app.state || '').toLowerCase().includes('processing'));
            const appsCompleted = sortedApps.filter(app => (app.state || '').toLowerCase() === 'completed');
            const appsFailed = sortedApps.filter(app => (app.state || '').toLowerCase() === 'failed' || app.error);
            
            renderAppsTable('chartRepoAppsTableBody', sortedApps);
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
            
            // Deduplicate tasks by task_id (keep the one with latest updated_at)
            const taskMap = new Map();
            tasks.forEach(task => {
                const taskId = task.task_id;
                if (!taskId) return;
                
                const existing = taskMap.get(taskId);
                if (!existing) {
                    taskMap.set(taskId, task);
                } else {
                    // Keep the one with latest updated_at
                    const existingTime = getChartRepoTaskUpdatedAtValue(existing);
                    const currentTime = getChartRepoTaskUpdatedAtValue(task);
                    if (currentTime > existingTime) {
                        taskMap.set(taskId, task);
                    }
                }
            });
            const uniqueTasks = Array.from(taskMap.values());
            
            const sortedTasks = [...uniqueTasks].sort((a, b) => getChartRepoTaskUpdatedAtValue(b) - getChartRepoTaskUpdatedAtValue(a));
            const tasksPending = sortedTasks.filter(task => (task.status || '').toLowerCase() === 'pending');
            const tasksRunning = sortedTasks.filter(task => (task.status || '').toLowerCase() === 'running');
            const tasksCompleted = sortedTasks.filter(task => (task.status || '').toLowerCase() === 'completed');
            const tasksFailed = sortedTasks.filter(task => (task.status || '').toLowerCase() === 'failed' || task.last_error);
            
            renderTasksTable('chartRepoTasksTableBody', sortedTasks);
            renderTasksTableSimple('chartRepoTasksPendingBody', tasksPending);
            renderTasksTable('chartRepoTasksRunningBody', tasksRunning);
            renderTasksTableSimple('chartRepoTasksCompletedBody', tasksCompleted);
            renderTasksTableWithError('chartRepoTasksFailedBody', tasksFailed);
        }
        
        function getChartRepoAppUpdatedAt(app) {
            if (!app) return null;
            if (app.timestamps && app.timestamps.last_updated_at) {
                return app.timestamps.last_updated_at;
            }
            if (app.last_update) {
                return app.last_update;
            }
            if (app.current_step && app.current_step.updated_at) {
                return app.current_step.updated_at;
            }
            return null;
        }
        
        function getChartRepoAppUpdatedAtValue(app) {
            const ts = getChartRepoAppUpdatedAt(app);
            if (!ts) return 0;
            const date = new Date(ts);
            const time = date.getTime();
            return isNaN(time) ? 0 : time;
        }
        
        function getChartRepoAppUpdatedAtLabel(app) {
            const ts = getChartRepoAppUpdatedAt(app);
            if (!ts) return 'N/A';
            return formatTimestamp(ts);
        }
        
        function getChartRepoTaskUpdatedAt(task) {
            if (!task) return null;
            if (task.updated_at) {
                return task.updated_at;
            }
            if (task.created_at) {
                return task.created_at;
            }
            return null;
        }
        
        function getChartRepoTaskUpdatedAtValue(task) {
            const ts = getChartRepoTaskUpdatedAt(task);
            if (!ts) return 0;
            const date = new Date(ts);
            const time = date.getTime();
            return isNaN(time) ? 0 : time;
        }
        
        function getChartRepoTaskUpdatedAtLabel(task) {
            const ts = getChartRepoTaskUpdatedAt(task);
            if (!ts) return 'N/A';
            return formatTimestamp(ts);
        }
        
        function renderAppsTable(tbodyId, apps) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (apps.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No applications</td></tr>';
                return;
            }
            apps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.state || 'unknown') + '</td>' +
                    '<td>' + (app.current_step ? app.current_step.name : '-') + '</td>' +
                    '<td>' + getChartRepoAppUpdatedAtLabel(app) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderAppsTableWithError(tbodyId, apps) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (apps.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No applications</td></tr>';
                return;
            }
            apps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = '<td>' + (app.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.source_id || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(app.state || 'unknown') + '</td>' +
                    '<td>' + (app.error ? app.error.message : '-') + '</td>' +
                    '<td>' + getChartRepoAppUpdatedAtLabel(app) + '</td>';
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
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No tasks</td></tr>';
                return;
            }
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.step_name || '-') + '</td>' +
                    '<td>' + (task.retry_count || 0) + '</td>' +
                    '<td>' + getChartRepoTaskUpdatedAtLabel(task) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableSimple(tbodyId, tasks) {
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
                    '<td>' + getChartRepoTaskUpdatedAtLabel(task) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function renderTasksTableWithError(tbodyId, tasks) {
            const tbody = document.getElementById(tbodyId);
            if (!tbody) return;
            tbody.innerHTML = '';
            if (tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No tasks</td></tr>';
                return;
            }
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = '<td style="font-size: 11px;">' + (task.task_id || 'N/A') + '</td>' +
                    '<td>' + (task.app_name || 'N/A') + '</td>' +
                    '<td>' + getStatusBadge(task.status || 'unknown') + '</td>' +
                    '<td>' + (task.step_name || '-') + '</td>' +
                    '<td style="font-size: 11px;">' + (task.last_error || '-') + '</td>' +
                    '<td>' + getChartRepoTaskUpdatedAtLabel(task) + '</td>';
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

// getRuntimeDashboardApp handles GET /api/v2/runtime/dashboard-app
// Returns an HTML page showing app processing flow status
func (s *Server) getRuntimeDashboardApp(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/runtime/dashboard-app - Getting app processing flow dashboard")

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
	html := generateDashboardAppHTML(string(snapshotJSON))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// generateDashboardAppHTML generates the HTML dashboard page for app processing flow
func generateDashboardAppHTML(snapshotJSON string) string {
	// Escape JSON for embedding in HTML
	escapedJSON := template.JSEscapeString(snapshotJSON)

	htmlTemplate := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>App Processing Flow Dashboard</title>
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
        
        .panel {
            background: #ffffff;
            border-radius: 8px;
            padding: 20px;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.08);
            margin-bottom: 16px;
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
        
        .badge-pending {
            background: #f3f4f6;
            color: #374151;
        }
        
        .flow-steps {
            display: flex;
            align-items: center;
            gap: 8px;
            flex-wrap: wrap;
        }
        
        .flow-step {
            display: flex;
            align-items: center;
            gap: 4px;
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 11px;
            font-weight: 500;
        }
        
        .flow-step.completed {
            background: #d1fae5;
            color: #065f46;
        }
        
        .flow-step.running {
            background: #dbeafe;
            color: #1e40af;
        }
        
        .flow-step.pending {
            background: #f3f4f6;
            color: #374151;
        }
        
        .flow-step.failed {
            background: #fee2e2;
            color: #991b1b;
        }
        
        .flow-step-arrow {
            color: #9ca3af;
            font-size: 12px;
        }
        
        .empty-state {
            text-align: center;
            padding: 40px 20px;
            color: #9ca3af;
            font-size: 14px;
        }
        
        .filter-controls {
            display: flex;
            gap: 12px;
            margin-bottom: 16px;
            flex-wrap: wrap;
        }
        
        .filter-input {
            padding: 8px 12px;
            border: 1px solid #e5e7eb;
            border-radius: 6px;
            font-size: 14px;
            min-width: 200px;
        }
        
        .filter-input:focus {
            outline: none;
            border-color: #2563eb;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>App Processing Flow Dashboard</h1>
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
        
        <div class="panel">
            <div class="panel-header">
                <h2>Application Processing Status</h2>
            </div>
            <div class="filter-controls">
                <input type="text" class="filter-input" id="filterAppName" placeholder="Filter by App Name" onkeyup="filterApps()">
                <input type="text" class="filter-input" id="filterUserID" placeholder="Filter by User ID" onkeyup="filterApps()">
                <input type="text" class="filter-input" id="filterSourceID" placeholder="Filter by Source ID" onkeyup="filterApps()">
            </div>
            <div class="table-container">
                <table id="appsTable">
                    <thead>
                        <tr>
                            <th>App Name</th>
                            <th>User ID</th>
                            <th>Source ID</th>
                            <th>Version</th>
                            <th>Processing Flow</th>
                            <th>Current Step</th>
                            <th>Status</th>
                            <th>Last Update</th>
                        </tr>
                    </thead>
                    <tbody id="appsTableBody"></tbody>
                </table>
            </div>
        </div>
    </div>
    
    <script>
        let snapshotData = {};
        try {
            snapshotData = JSON.parse('%s');
            // Debug: log data structure
            console.log('[DEBUG] Snapshot data loaded:', snapshotData);
            console.log('[DEBUG] App states count:', snapshotData.app_states ? Object.keys(snapshotData.app_states).length : 0);
            console.log('[DEBUG] Components:', snapshotData.components ? Object.keys(snapshotData.components) : []);
            console.log('[DEBUG] Chart repo:', snapshotData.chart_repo ? 'present' : 'missing');
        } catch (e) {
            console.error('Failed to parse snapshot data:', e);
            snapshotData = {};
        }
        let autoRefreshInterval = null;
        
        // Processing flow steps definition
        const PROCESSING_STEPS = [
            { name: 'Syncer', key: 'syncer' },
            { name: 'Market Hydration', key: 'market_hydration' },
            { name: 'SourceChartStep', key: 'source_chart_step' },
            { name: 'RenderedChartStep', key: 'rendered_chart_step' },
            { name: 'CustomParamsUpdateStep', key: 'custom_params_update_step' },
            { name: 'ImageAnalysisStep', key: 'image_analysis_step' },
            { name: 'DatabaseUpdateStep', key: 'database_update_step' }
        ];
        
        function getStatusBadge(status) {
            if (!status) return '<span class="badge badge-pending">Unknown</span>';
            const lower = status.toLowerCase();
            if (lower === 'completed' || lower === 'success' || lower === 'healthy') {
                return '<span class="badge badge-success">' + status + '</span>';
            } else if (lower === 'running' || lower === 'processing' || lower === 'active') {
                return '<span class="badge badge-info">' + status + '</span>';
            } else if (lower === 'failed' || lower === 'error' || lower === 'unhealthy') {
                return '<span class="badge badge-danger">' + status + '</span>';
            } else if (lower === 'pending' || lower === 'waiting') {
                return '<span class="badge badge-warning">' + status + '</span>';
            }
            return '<span class="badge badge-pending">' + status + '</span>';
        }
        
        function formatTimestamp(timestamp) {
            if (!timestamp) return 'N/A';
            const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp;
            if (isNaN(date.getTime())) return 'N/A';
            return date.toLocaleString('zh-CN', { 
                year: 'numeric', 
                month: '2-digit', 
                day: '2-digit', 
                hour: '2-digit', 
                minute: '2-digit', 
                second: '2-digit' 
            });
        }
        
        function getAppProcessingFlow(appKey, appState, chartRepoApps, chartRepoHydratorTasks) {
            const steps = [];
            
            // Step 1: Syncer
            const syncerStatus = getSyncerStatus();
            steps.push({
                name: 'Syncer',
                status: syncerStatus,
                key: 'syncer'
            });
            
            // Step 2: Market Hydration
            const marketHydrationStatus = getMarketHydrationStatus(appKey);
            steps.push({
                name: 'Market Hydration',
                status: marketHydrationStatus,
                key: 'market_hydration'
            });
            
            // Step 3-7: ChartRepo Hydration Steps
            const chartRepoApp = findChartRepoApp(appKey, chartRepoApps);
            const chartRepoTask = findChartRepoTask(appKey, chartRepoHydratorTasks);
            
            if (chartRepoApp || chartRepoTask) {
                // Use chart repo app state if available, otherwise use task state
                let currentStep = null;
                let stepIndex = -1;
                
                if (chartRepoApp && chartRepoApp.current_step) {
                    currentStep = chartRepoApp.current_step;
                    stepIndex = currentStep.index;
                } else if (chartRepoTask) {
                    // Map step name to index
                    const stepNameToIndex = {
                        'SourceChartStep': 0,
                        'RenderedChartStep': 1,
                        'CustomParamsUpdateStep': 2,
                        'ImageAnalysisStep': 3,
                        'DatabaseUpdateStep': 4
                    };
                    const stepName = chartRepoTask.step_name || '';
                    stepIndex = stepNameToIndex[stepName] !== undefined ? stepNameToIndex[stepName] : -1;
                    
                    if (chartRepoTask.status === 'running') {
                        currentStep = { status: 'running' };
                    } else if (chartRepoTask.status === 'completed') {
                        currentStep = { status: 'completed' };
                    } else if (chartRepoTask.status === 'failed') {
                        currentStep = { status: 'failed' };
                    }
                }
                
                const chartRepoSteps = [
                    { name: 'SourceChartStep', index: 0 },
                    { name: 'RenderedChartStep', index: 1 },
                    { name: 'CustomParamsUpdateStep', index: 2 },
                    { name: 'ImageAnalysisStep', index: 3 },
                    { name: 'DatabaseUpdateStep', index: 4 }
                ];
                
                chartRepoSteps.forEach((step) => {
                    let status = 'pending';
                    if (stepIndex > step.index) {
                        status = 'completed';
                    } else if (stepIndex === step.index && currentStep) {
                        status = currentStep.status.toLowerCase();
                    }
                    steps.push({
                        name: step.name,
                        status: status,
                        key: step.name.toLowerCase().replace('step', '_step')
                    });
                });
            } else {
                // No chart repo app or task found, all steps pending
                const chartRepoSteps = [
                    'SourceChartStep',
                    'RenderedChartStep',
                    'CustomParamsUpdateStep',
                    'ImageAnalysisStep',
                    'DatabaseUpdateStep'
                ];
                chartRepoSteps.forEach(stepName => {
                    steps.push({
                        name: stepName,
                        status: 'pending',
                        key: stepName.toLowerCase().replace('step', '_step')
                    });
                });
            }
            
            return steps;
        }
        
        function getSyncerStatus() {
            const components = snapshotData.components || {};
            const syncer = components.syncer;
            if (!syncer) return 'unknown';
            if (!syncer.healthy) return 'failed';
            if (syncer.status === 'running') return 'running';
            return syncer.status || 'unknown';
        }
        
        function getMarketHydrationStatus(appKey) {
            const components = snapshotData.components || {};
            const hydrator = components.hydrator;
            if (!hydrator || !hydrator.metrics) return 'pending';
            
            const activeTasks = hydrator.metrics.active_tasks || [];
            const [userID, sourceID, appName] = appKey.split(':');
            
            // Find active task for this app
            const activeTask = activeTasks.find(task => {
                return task.user_id === userID && 
                       task.source_id === sourceID && 
                       (task.app_id === appName || task.app_name === appName);
            });
            
            if (activeTask) {
                return 'running';
            }
            
            // Check completed tasks
            const completedTasks = hydrator.metrics.recent_completed_tasks || [];
            const completedTask = completedTasks.find(task => {
                return task.user_id === userID && 
                       task.source_id === sourceID && 
                       (task.app_id === appName || task.app_name === appName);
            });
            
            if (completedTask) {
                return 'completed';
            }
            
            // Check failed tasks
            const failedTasks = hydrator.metrics.recent_failed_tasks || [];
            const failedTask = failedTasks.find(task => {
                return task.user_id === userID && 
                       task.source_id === sourceID && 
                       (task.app_id === appName || task.app_name === appName);
            });
            
            if (failedTask) {
                return 'failed';
            }
            
            return 'pending';
        }
        
        function findChartRepoApp(appKey, chartRepoApps) {
            if (!chartRepoApps || !Array.isArray(chartRepoApps)) return null;
            const [userID, sourceID, appName] = appKey.split(':');
            return chartRepoApps.find(app => {
                return app.user_id === userID && 
                       app.source_id === sourceID && 
                       (app.app_id === appName || app.app_name === appName);
            });
        }
        
        function findChartRepoTask(appKey, chartRepoHydratorTasks) {
            if (!chartRepoHydratorTasks || !Array.isArray(chartRepoHydratorTasks)) return null;
            const [userID, sourceID, appName] = appKey.split(':');
            return chartRepoHydratorTasks.find(task => {
                return task.user_id === userID && 
                       task.source_id === sourceID && 
                       (task.app_id === appName || task.app_name === appName);
            });
        }
        
        function renderFlowSteps(steps) {
            let html = '<div class="flow-steps">';
            steps.forEach((step, index) => {
                const statusClass = step.status || 'pending';
                html += '<div class="flow-step ' + statusClass + '">';
                html += '<span>' + step.name + '</span>';
                html += '</div>';
                if (index < steps.length - 1) {
                    html += '<span class="flow-step-arrow">→</span>';
                }
            });
            html += '</div>';
            return html;
        }
        
        function getCurrentStepName(steps) {
            const runningStep = steps.find(step => step.status === 'running');
            if (runningStep) return runningStep.name;
            const failedStep = steps.find(step => step.status === 'failed');
            if (failedStep) return failedStep.name + ' (Failed)';
            const lastCompleted = steps.filter(step => step.status === 'completed').pop();
            if (lastCompleted) return lastCompleted.name + ' (Completed)';
            return 'Not Started';
        }
        
        function getOverallStatus(steps) {
            const hasFailed = steps.some(step => step.status === 'failed');
            if (hasFailed) return 'failed';
            const hasRunning = steps.some(step => step.status === 'running');
            if (hasRunning) return 'running';
            const allCompleted = steps.every(step => step.status === 'completed');
            if (allCompleted) return 'completed';
            return 'pending';
        }
        
        function getAllProcessingApps() {
            const appMap = new Map(); // key: userID:sourceID:appName -> app info
            
            // 1. Get apps from app_states (installed apps)
            const appStates = snapshotData.app_states || {};
            Object.entries(appStates).forEach(([key, appState]) => {
                if (appState) {
                    appMap.set(key, {
                        key: key,
                        app_name: appState.app_name || '',
                        user_id: appState.user_id || '',
                        source_id: appState.source_id || '',
                        version: appState.version || '',
                        last_update: appState.last_update || null,
                        from: 'app_states'
                    });
                }
            });
            
            // 2. Get apps from syncer last_sync_details
            const components = snapshotData.components || {};
            const syncer = components.syncer;
            if (syncer && syncer.metrics && syncer.metrics.last_sync_details) {
                const syncDetails = syncer.metrics.last_sync_details;
                const sourceID = syncDetails.source_id || '';
                
                // Add succeeded apps
                if (syncDetails.succeeded_apps && Array.isArray(syncDetails.succeeded_apps)) {
                    syncDetails.succeeded_apps.forEach(appID => {
                        const key = ':' + sourceID + ':' + appID;
                        if (!appMap.has(key)) {
                            appMap.set(key, {
                                key: key,
                                app_name: appID,
                                user_id: '',
                                source_id: sourceID,
                                version: '',
                                last_update: syncDetails.sync_time || null,
                                from: 'syncer_succeeded'
                            });
                        }
                    });
                }
                
                // Add failed apps
                if (syncDetails.failed_apps && Array.isArray(syncDetails.failed_apps)) {
                    syncDetails.failed_apps.forEach(failedApp => {
                        const appID = failedApp.app_id || failedApp.app_name || '';
                        const key = ':' + sourceID + ':' + appID;
                        if (!appMap.has(key)) {
                            appMap.set(key, {
                                key: key,
                                app_name: failedApp.app_name || appID,
                                user_id: '',
                                source_id: sourceID,
                                version: '',
                                last_update: syncDetails.sync_time || null,
                                from: 'syncer_failed'
                            });
                        }
                    });
                }
            }
            
            // 3. Get apps from hydrator active_tasks
            const hydrator = components.hydrator;
            if (hydrator && hydrator.metrics) {
                const activeTasks = hydrator.metrics.active_tasks || [];
                activeTasks.forEach(task => {
                    if (task) {
                        const key = (task.user_id || '') + ':' + (task.source_id || '') + ':' + (task.app_id || task.app_name || '');
                        if (!appMap.has(key)) {
                            appMap.set(key, {
                                key: key,
                                app_name: task.app_name || task.app_id || '',
                                user_id: task.user_id || '',
                                source_id: task.source_id || '',
                                version: '',
                                last_update: task.started_at || null,
                                from: 'hydrator_active'
                            });
                        }
                    }
                });
                
                // Get apps from hydrator recent_completed_tasks
                const completedTasks = hydrator.metrics.recent_completed_tasks || [];
                completedTasks.forEach(task => {
                    if (task) {
                        const key = (task.user_id || '') + ':' + (task.source_id || '') + ':' + (task.app_id || task.app_name || '');
                        if (!appMap.has(key)) {
                            appMap.set(key, {
                                key: key,
                                app_name: task.app_name || task.app_id || '',
                                user_id: task.user_id || '',
                                source_id: task.source_id || '',
                                version: '',
                                last_update: task.completed_at || null,
                                from: 'hydrator_completed'
                            });
                        }
                    }
                });
                
                // Get apps from hydrator recent_failed_tasks
                const failedTasks = hydrator.metrics.recent_failed_tasks || [];
                failedTasks.forEach(task => {
                    if (task) {
                        const key = (task.user_id || '') + ':' + (task.source_id || '') + ':' + (task.app_id || task.app_name || '');
                        if (!appMap.has(key)) {
                            appMap.set(key, {
                                key: key,
                                app_name: task.app_name || task.app_id || '',
                                user_id: task.user_id || '',
                                source_id: task.source_id || '',
                                version: '',
                                last_update: task.completed_at || null,
                                from: 'hydrator_failed'
                            });
                        }
                    }
                });
            }
            
            // 4. Get apps from chart_repo.apps
            const chartRepo = snapshotData.chart_repo || {};
            const chartRepoApps = chartRepo.apps || [];
            chartRepoApps.forEach(app => {
                if (app) {
                    const key = (app.user_id || '') + ':' + (app.source_id || '') + ':' + (app.app_id || app.app_name || '');
                    if (!appMap.has(key)) {
                        appMap.set(key, {
                            key: key,
                            app_name: app.app_name || app.app_id || '',
                            user_id: app.user_id || '',
                            source_id: app.source_id || '',
                            version: '',
                            last_update: app.last_update || (app.timestamps && app.timestamps.last_updated_at) || null,
                            from: 'chart_repo_apps'
                        });
                    }
                }
            });
            
            // 5. Get apps from chart_repo.tasks.hydrator.tasks
            const chartRepoHydrator = chartRepo.tasks && chartRepo.tasks.hydrator;
            if (chartRepoHydrator && chartRepoHydrator.tasks) {
                chartRepoHydrator.tasks.forEach(task => {
                    if (task) {
                        const key = (task.user_id || '') + ':' + (task.source_id || '') + ':' + (task.app_id || task.app_name || '');
                        if (!appMap.has(key)) {
                            appMap.set(key, {
                                key: key,
                                app_name: task.app_name || task.app_id || '',
                                user_id: task.user_id || '',
                                source_id: task.source_id || '',
                                version: '',
                                last_update: task.updated_at || task.created_at || null,
                                from: 'chart_repo_tasks'
                            });
                        }
                    }
                });
            }
            
            return Array.from(appMap.values());
        }
        
        function renderApps() {
            const tbody = document.getElementById('appsTableBody');
            if (!tbody) return;
            
            // Get all processing apps from multiple sources
            const allApps = getAllProcessingApps();
            
            const chartRepo = snapshotData.chart_repo || {};
            const chartRepoApps = chartRepo.apps || [];
            const hydrator = chartRepo.tasks && chartRepo.tasks.hydrator;
            const chartRepoHydratorTasks = hydrator ? (hydrator.tasks || []) : [];
            
            const apps = allApps.map(appInfo => {
                // Create a minimal appState for compatibility
                const appState = {
                    app_name: appInfo.app_name,
                    user_id: appInfo.user_id,
                    source_id: appInfo.source_id,
                    version: appInfo.version,
                    last_update: appInfo.last_update
                };
                
                const flowSteps = getAppProcessingFlow(appInfo.key, appState, chartRepoApps, chartRepoHydratorTasks);
                return {
                    key: appInfo.key,
                    appState: appState,
                    flowSteps: flowSteps,
                    currentStep: getCurrentStepName(flowSteps),
                    overallStatus: getOverallStatus(flowSteps),
                    from: appInfo.from
                };
            });
            
            // Apply filters
            const filteredApps = apps.filter(app => {
                const appName = app.appState.app_name || '';
                const userID = app.appState.user_id || '';
                const sourceID = app.appState.source_id || '';
                
                const filterAppName = document.getElementById('filterAppName').value.toLowerCase();
                const filterUserID = document.getElementById('filterUserID').value.toLowerCase();
                const filterSourceID = document.getElementById('filterSourceID').value.toLowerCase();
                
                return appName.toLowerCase().includes(filterAppName) &&
                       userID.toLowerCase().includes(filterUserID) &&
                       sourceID.toLowerCase().includes(filterSourceID);
            });
            
            tbody.innerHTML = '';
            
            if (filteredApps.length === 0) {
                tbody.innerHTML = '<tr><td colspan="8" class="empty-state">No applications found</td></tr>';
                return;
            }
            
            filteredApps.forEach(app => {
                const row = document.createElement('tr');
                row.innerHTML = 
                    '<td>' + (app.appState.app_name || 'N/A') + '</td>' +
                    '<td>' + (app.appState.user_id || 'N/A') + '</td>' +
                    '<td>' + (app.appState.source_id || 'N/A') + '</td>' +
                    '<td>' + (app.appState.version || 'N/A') + '</td>' +
                    '<td>' + renderFlowSteps(app.flowSteps) + '</td>' +
                    '<td>' + app.currentStep + '</td>' +
                    '<td>' + getStatusBadge(app.overallStatus) + '</td>' +
                    '<td>' + formatTimestamp(app.appState.last_update) + '</td>';
                tbody.appendChild(row);
            });
        }
        
        function filterApps() {
            renderApps();
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
            renderApps();
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
