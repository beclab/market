// This is just an example,
// so you can safely delete all default props below

export default {
	base: {
		failed: 'Action failed',
		success: 'Action successful',
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
		confirm: 'Confirm',
		message: 'Message',
		scope: 'Scope',
		cluster_app: 'Cluster App',
		developer: 'Developer',
		language: 'Language',
		search: 'Search',
		category: 'Category',
		submitter: 'Submitter',
		documents: 'Documents',
		see_all: 'See all',
		more: 'More'
	},
	main: {
		discover: 'Discover',
		productivity: 'Productivity',
		utilities: 'Utilities',
		entertainment: 'Entertainment',
		social_network: 'Social network',
		blockchain: 'Blockchain',
		recommendation: 'Recommendation',
		recommend: 'Recommend',
		models: 'Models',
		large_language_model: 'Large Language Model',
		my_terminus: 'My Olares',
		terminus: 'Olares',
		terminus_market: 'Olares Market',
		install_terminus_os: 'Install Olares'
	},
	app: {
		get: 'Get',
		install: 'Install',
		installed: 'Installed',
		suspend: 'Suspend',
		crash: 'Crash',
		cancel: 'Cancel',
		load: 'Load',
		unload: 'Unload',
		resume: 'Resume',
		installing: 'Installing',
		initializing: 'Initializing',
		resuming: 'Resuming',
		update: 'Update',
		running: 'Running',
		open: 'Open',
		uninstalling: 'Uninstalling',
		uninstall: 'Uninstall',
		updating: 'Updating'
	},
	detail: {
		require_memory: 'Required memory',
		require_disk: 'Required disk',
		require_cpu: 'Required CPU',
		require_gpu: 'Required GPU',
		about_this_type: 'About this {type}',
		whats_new: "What's new",
		required_permissions: 'Required permissions',
		readme: 'Readme',
		information: 'Information',
		get_support: 'Get support',
		website: 'Website',
		app_version: 'App version',
		compatibility: 'Compatibility',
		platforms: 'Platforms',
		source_code: 'Source code',
		public: 'Public',
		legal: 'Legal',
		license: 'License',
		chart_version: 'Chart version',
		version_history: 'Version history',
		get_a_client: 'Get a client',
		dependency: 'Dependency',
		reference_app: 'Reference apps',
		see_all_version: 'See all versions',
		download: 'Download',
		no_version_history_desc: 'The application has no version history'
	},
	permission: {
		files: 'Files',
		files_not_store_label: 'This app does not store any files on Olares',
		files_access_user_data_label: 'Permission required to access user data',
		files_access_user_data_desc:
			'Allow this app to read and write files in the following directories:',
		file_store_data_folder_label:
			'Permission required to access the Data folder',
		file_store_data_folder_desc:
			'Allow this app to store persistent files in app-specific directories',
		file_store_cache_folder_label:
			'Permission required to access the Cache folder',
		file_store_cache_folder_desc:
			'Allow this app to store cache files in app-specific directories',
		internet: 'Internet',
		internet_label:
			'Full network access required during installing and operation',
		internet_desc:
			'Allow this app to connect to, download from, and upload data to the Internet while installing and operation',
		notifications: 'Notifications',
		notifications_label: 'Permission required to send you notifications',
		notifications_desc:
			'Allow this app to send notifications, including Olares Messages, SMS, Emails, and Third-party IM Messages. You can configure these preferences in Settings.',
		analytics: 'Analytics',
		analytics_label:
			'This app uses built-in tools to collect metrics you need for website analytics',
		analytics_desc:
			'No personal information is collected, and all web data is anonymized and stored locally on your Olares',
		websocket: 'Websocket',
		websocket_label:
			'Permission required to use WebSocket for a two-way interactive communication between browser and Olares',
		websocket_desc:
			'Allow this app to send content to the webpage in your browser via WebSocket',
		secret: 'Secret',
		secret_label:
			'Create and manage application configuration and secrets using Vault',
		secret_desc:
			"Vault is Olares's secret management app for storing, managing, and syncing sensitive information such as API keys, database credentials, and environment variables",
		knowledgebase: 'Knowledge base',
		knowledgebase_label:
			'Permission required to access your local knowledge base',
		knowledgebase_desc:
			'Allows this app to use your personal data stored in Olares local knowledge base',
		search_label: "This app supports Olares's full-text search engine",
		relational_database: 'Relational database',
		relational_database_label:
			'This app uses PostgreSQL as its relational database',
		relational_database_desc:
			'PostgreSQL is provided by Olares Middleware Service.',
		document_database: 'Document database',
		document_database_label: 'This app uses MongoDB as its document database',
		document_database_desc: 'MongoDB is provided by Olares Middleware Service.',
		key_value_database: 'Key-value database',
		key_value_database_label: 'This app uses Redis as its Key-value database',
		key_value_database_desc: 'Redis is provided by Olares Middleware Service.',
		cluster_app_label:
			'This app will be installed at the cluster level, and shared by all users in the same Olares cluster',
		entrance: 'Entrance',
		entrance_visibility_label:
			'Number of different visibility entrances for this app: {desktopSize} visible and {backendSize} invisible',
		entrance_visibility_desc_first:
			"The visible entrance allows you to access the app's webpage through Desktop",
		entrance_visibility_desc_second:
			'The invisible entrance typically operates in the backend and is used for the app to interact with other apps.',
		entrance_auth_level_label:
			'Number of entrances of different auth levels: {publicSize} public, {privateSize} private',
		entrance_auth_level_desc:
			'A public entrance is accessible to anyone on the Internet. A private entrance requires activation of Tailscale for access.',
		entrance_two_factor_label:
			'These entrances require Two-Factor Authentication to access: {twoFactor}'
	},
	error: {
		network_error: 'Network error. Please try again later.',
		unknown_error: 'Unknown error',
		failed_get_user_role: 'Failed to get user role',
		only_be_installed_by_the_admin: 'This app can only be installed by Admin',
		not_admin_role_install_middleware:
			'Middleware component. Contact your Olares Admin to install.',
		not_admin_role_install_cluster_app:
			'Cluster app. Contact your Olares Admin to install.',
		failed_to_get_os_version: 'Failed to get Olares version',
		app_is_not_compatible_terminus_os: 'Incompatible with your Olares',
		failed_to_get_user_resource: 'Failed to get user resource',
		user_not_enough_cpu: 'Insufficient CPU on your quota',
		user_not_enough_memory: 'Insufficient memory on your quota',
		failed_to_get_system_resource: 'Failed to get system resource',
		need_to_install_dependent_app_first: 'Need to install dependent app first',
		terminus_not_enough_cpu: 'Insufficient CPU on the Olares cluster',
		terminus_not_enough_memory: 'Insufficient memory on the Olares cluster',
		terminus_not_enough_disk: 'Insufficient disk on the Olares cluster',
		terminus_not_enough_gpu: 'Insufficient GPU on the Olares cluster',
		operation_preform_failure: 'Operation failed',
		cluster_not_support_platform:
			'This [app/recommend/model] does not support your Olares platform'
	},
	my: {
		market: 'Market',
		custom: 'Custom',
		unable_to_install_app: 'Unable to install app',
		update_all: 'Update all',
		available_updates: 'Available updates',
		everything_up_to_date: 'Congratulations! Everything is up to date.',
		no_upload_chart_tips: "You haven't uploaded any custom charts yet",
		no_installed_app_tips: 'You haven’t installed anything yet',
		no_logs: 'No installation logs available',
		sure_to_uninstall_the_app: "Are you sure to uninstall '{title}'？",
		application_has_crashed:
			'The application has crashed. Please wait while it restarts.',
		application_has_been_suspended:
			'This application has been suspended. You can resume it to open.',
		upload_custom_chart: 'Upload custom chart',
		logs: 'Logs'
	},

	//temp
	discover_amazing_apps: 'Discover amazing apps',
	featured_app: 'FEATURED APP',
	control_your_own_social_network: 'Control your own social network.',
	decentralized_social_media: 'Decentralized social media',
	get_started: 'GET STARTED',
	booster_your_software_development_productivity:
		'Boost your software development productivity.',
	enjoy_coding_now: 'Enjoy coding now',
	diving_into_ai_image_generation: 'Dive into AI image generation.',
	unleashing_your_creativity: 'Unleash your creativity!',
	mastering_your_photo_with_a_personal_library:
		'Master your photos with a personal library.',
	organize_your_memories: 'Organize your memories',
	nocode_solution_for_your_data_management:
		'No-code solution for your data management.',
	build_databases_as_spreadsheets: 'Build databases as spreadsheets',
	top_app_in: 'Top apps in {category}',
	latest_app_in: 'Latest apps in {category}',
	communitys_choices: 'Community choices',
	top_app_on_terminus: 'Top apps on Olares',
	latest_app_on_terminus: 'Latest apps on Olares',
	featured_apps_in_discover: 'Featured apps in Discover',
	featured_apps_in_productivity: 'Featured apps in Productivity',
	featured_apps_in_utilities: 'Featured apps in Utilities',
	featured_apps_in_entertainment: 'Featured apps in Entertainment',
	featured_apps_in_social_network: 'Featured apps in Social Network'
};
