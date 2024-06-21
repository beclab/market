// This is just an example,
// so you can safely delete all default props below

export default {
	base: {
		failed: 'Action failed',
		success: 'Action was successful',
		application: 'Application',
		extensions: 'Categories',
		time: 'Time',
		type: 'Type',
		title: 'Title',
		version: 'Version',
		operations: 'Operations',
		result: 'Result',
		status: 'Status',
		name: 'Name',
		message: 'Message',
		scope: 'Scope',
		cluster_app: 'Cluster App',
		developer: 'Developer',
		language: 'Language',
		search: 'Search',
		category: 'Category',
		submitter: 'Submitter',
		documents: 'Documents'
	},
	main: {
		discover: 'Discover',
		productivity: 'Productivity',
		utilities: 'Utilities',
		entertainment: 'Entertainment',
		social_network: 'Social Network',
		blockchain: 'Blockchain',
		recommendation: 'Recommendation',
		recommend: 'Recommend',
		models: 'Models',
		my_terminus: 'My Terminus',
		terminus: 'Terminus',
		terminus_market: 'Terminus Market',
		install_terminus_os: 'Install TerminusOS'
	},
	agent: {
		large_language_model: 'Large Language Model',
		trending_llms: 'Trending LLMs',
		trending_agents: 'Trending Agents'
	},
	app: {
		get: 'Get',
		install: 'Install',
		installed: 'Installed',
		suspend: 'Suspend',
		cancel: 'Cancel',
		load: 'Load',
		unload: 'Unload',
		resume: 'Resume',
		installing: 'Installing',
		update: 'Update',
		running: 'Running',
		open: 'Open',
		uninstalling: 'Uninstalling',
		uninstall: 'Uninstall',
		updating: 'Updating'
	},
	detail: {
		require_memory: 'Require Memory',
		require_disk: 'Require Disk',
		require_cpu: 'Require CPU',
		require_gpu: 'Require GPU',
		about_this_type: 'ABOUT THIS {type}',
		whats_new: 'WHATS NEW',
		required_permissions: 'REQUIRED PERMISSIONS',
		readme: 'README',
		information: 'INFORMATION',
		get_support: 'Get Support',
		website: 'Website',
		app_version: 'App Version',
		compatibility: 'Compatibility',
		platforms: 'Platforms',
		source_code: 'Source Code',
		public: 'Public',
		legal: 'Legal',
		license: 'License',
		chart_version: 'Chart Version',
		version_history: 'Version History',
		get_a_client: 'GET A CLIENT',
		dependency: 'DEPENDENCY',
		reference_app: 'REFERENCE APP'
	},
	permission: {
		files: 'Files',
		files_not_store_label: 'This app will not store any files on Termius.',
		files_access_user_data_label: 'Requires permission to access User Data.',
		files_access_user_data_desc:
			'Allow this app to read and write files in the following directories:',
		file_store_data_folder_label: 'Requires permission to access Data folder.',
		file_store_data_folder_desc:
			'Allow this app to store persistent files in app-specific directories.',
		file_store_cache_folder_label:
			'Requires permission to access Cache folder.',
		file_store_cache_folder_desc:
			'Allow this app to store cache files in app-specific directories.',
		internet: 'Internet',
		internet_label: 'Full network access while installing and running.',
		internet_desc:
			'Allow this app to connect to, download from, and upload data to the Internet while installing and running.',
		notifications: 'Notifications',
		notifications_label: 'Permission to send you notifications.',
		notifications_desc:
			'Allow this app to send you notifications, such as Terminus Messages, SMS, Emails, Third-party IM Messages. These can be configured in Settings.',
		analytics: 'Analytics',
		analytics_label:
			'This app uses built-in tools to collect metrics you need for website analytics.',
		analytics_desc:
			"It will not collect your visitors' personal information, and all web data will be anonymized and stored locally on your Terminus.",
		websocket: 'Websocket',
		websocket_label:
			'Permission to use WebSocket for a two-way interactive communication between browser and Terminus.',
		websocket_desc:
			'Allow this app to send content to the webpage in your browser via WebSocket protocol.',
		secret: 'Secret',
		secret_label:
			'Create and manage application configuration and secrets using Vault.',
		secret_desc:
			'Vault is a Terminus secret management platform for storing, managing, and syncing sensitive information, such as API keys, database credentials, and environment variables.',
		knowledgebase: 'Knowledgebase',
		knowledgebase_label:
			'Permission to use the data in your local knowledge base.',
		knowledgebase_desc:
			'Allow this app to use your personal data stored in Terminus local knowledge base, such as articles from subscribed RSS feeds and PDFs uploaded to Wise.',
		search_label: 'This app supports Terminus full-text search engine.',
		relational_database: 'Relational Database',
		relational_database_label:
			'This app uses PostgreSQL as its relational database',
		relational_database_desc:
			'PostgreSQL is provided by Terminus Middleware Service.',
		document_database: 'Document Database',
		document_database_label: 'This app uses MongoDB as its document database',
		document_database_desc:
			'MongoDB is provided by Terminus Middleware Service.',
		key_value_database: 'Key-Value Database',
		key_value_database_label: 'This app uses Redis as its Key-Value  database',
		key_value_database_desc:
			'Redis is provided by Terminus Middleware Service.',
		cluster_app_label:
			'This app will be installed at cluster level, and shared by all users in the same Terminus cluster.',
		entrance: 'Entrance',
		entrance_visibility_label:
			'Number of different visibility entrances for this app: {desktopSize} visible, {backendSize} invisible.',
		entrance_visibility_desc_first:
			"The visible entrance allows you to access the app's UI webpage through the Terminus desktop.",
		entrance_visibility_desc_second:
			'The invisible entrance typically operates in the backend and is used for the app to interact with other apps.',
		entrance_auth_level_label:
			'Number of different auth Level entrances for this app: {publicSize} public, {privateSize} private.',
		entrance_auth_level_desc:
			'Public entrance is accessible to anyone on the Internet, whereas private entrance requires activation of Tailscale for access.',
		entrance_two_factor_label:
			'These entrances require Two-Factor Authentication to access: {twoFactor}'
	},
	error: {
		network_error: 'Network error, please try again later',
		unknown_error: 'Unknown error',
		failed_get_user_role: 'Failed to get user role',
		only_be_installed_by_the_admin:
			'This app can only be installed by the Admin.',
		not_admin_role_install_middleware:
			'This is a middleware, please contact your Terminus Admin to install.',
		not_admin_role_install_cluster_app:
			'This is a cluster app, please contact your Terminus Admin to install.',
		failed_to_get_os_version: 'Failed to get Terminus Version',
		app_is_not_compatible_terminus_os:
			'This app isn’t compatible with your Terminus OS.',
		failed_to_get_user_resource: 'Failed to get user resource',
		user_not_enough_cpu: 'Not enough required CPU on your quota.',
		user_not_enough_memory: 'Not enough required memory on your quota.',
		failed_to_get_system_resource: 'Failed to get system resource',
		need_to_install_dependent_app_first: 'Need to install dependent app first.',
		terminus_not_enough_cpu: 'Not enough required CPU on Terminus cluster.',
		terminus_not_enough_memory:
			'Not enough required memory on Terminus cluster.',
		terminus_not_enough_disk: 'Not enough required disk on Terminus cluster.',
		terminus_not_enough_gpu: 'Not enough required GPU on Terminus cluster.',
		operation_preform_failure: 'operation preform failure',
		cluster_not_support_platform:
			'This [app/recommend/model] does not support your Terminus OS platform architecture.'
	},
	unable_to_install_app: 'Unable to install app',
	update_all: 'Update All',
	available_updates: 'Available Updates',
	everything_up_to_date: 'Congrats, everything is up to date!',
	version_history: 'Version History',
	no_version_history_desc: 'The application has no version history.',
	no_upload_chart_tips: 'You haven’t uploaded any custom chart yet.',
	no_installed_app_tips: 'You haven’t installed anything yet.',
	no_logs: 'There are no installation logs here.',
	see_all: 'See All',
	sure_to_uninstall_the_app:
		'Are you sure you want to uninstall the application {title}？'
};
