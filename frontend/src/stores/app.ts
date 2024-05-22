import { defineStore } from 'pinia';

import { getApp, getAppsByNames, getPage } from 'src/api/storeApi';

import {
	cancelInstalling,
	installApp,
	resumeApp,
	suspendApp,
	uninstallApp,
	upgradeApp
} from 'src/api/private/operations';

import {
	getAppsBriefInfoByStatus,
	getInstalledApps
} from 'src/api/private/user';

import {
	APP_STATUS,
	AppStoreInfo,
	CATEGORIES_TYPE,
	CFG_TYPE,
	OPERATE_ACTION,
	OPERATE_STATUS,
	SOURCE_TYPE
} from '../constants/constants';
import { bus, BUS_EVENT } from 'src/utils/bus';
import { updateCategoryData } from 'src/utils/dataUtils';
import { AsyncQueue } from 'src/utils/asyncQueue';
import { useUserStore } from 'src/stores/user';
import { i18n } from 'src/boot/i18n';
import { decodeUnicode } from 'src/utils/utils';

export type AppState = {
	tempAppMap: Record<string, AppStoreInfo>;
	initialized: boolean;
	pageData: [] | null;
	installApps: AppStoreInfo[];
	updateApps: AppStoreInfo[];
	queue: AsyncQueue;
	isPublic: boolean;
};

export const useAppStore = defineStore('app', {
	state: () => {
		return {
			tempAppMap: {},
			initialized: false,
			installApps: [],
			updateApps: [],
			pageData: null,
			isPublic: !!process.env.PUBLIC_URL,
			queue: new AsyncQueue()
		} as AppState;
	},
	actions: {
		async init() {
			console.log('app init');
			await this.loadApps();
			console.log('app init requests completed');
			this.initialized = true;
		},

		async loadApps() {
			await this._getInstalledApps();
			await this._getUpdateApps();
		},

		async getPageData(category: string): Promise<any> {
			if (!this.pageData) {
				this.pageData = await getPage();
			}
			if (!this.pageData || this.pageData.length == 0) {
				return null;
			}
			switch (category) {
				case CATEGORIES_TYPE.LOCAL.ALL:
				case CATEGORIES_TYPE.SERVER.Productivity:
				case CATEGORIES_TYPE.SERVER.Utilities:
				case CATEGORIES_TYPE.SERVER.Entertainment:
				case CATEGORIES_TYPE.SERVER.Blockchain:
				case CATEGORIES_TYPE.SERVER.SocialNetwork:
				case CATEGORIES_TYPE.SERVER.LifeStyle:
				case CATEGORIES_TYPE.SERVER.News:
				case CATEGORIES_TYPE.SERVER.Sports:
					return this.pageData.find((item: any) => item.category === category);
				default:
					return null;
			}
		},

		setAppItem(app: AppStoreInfo | null) {
			if (!app || !app.name) {
				return;
			}
			this.tempAppMap[app.name] = app;
		},
		getAppItem(name: string) {
			return this.tempAppMap[name];
		},
		removeAppItem(name: string) {
			delete this.tempAppMap[name];
		},

		//temp function
		async addDevApplication(name: string) {
			const app = await getApp(name);
			if (
				app &&
				app.source === SOURCE_TYPE.Development &&
				app.cfgType === CFG_TYPE.WORK_FLOW
			) {
				this.installApps.unshift(app);
			}
		},

		async installApp(app: AppStoreInfo, isDev: boolean) {
			app.status = APP_STATUS.pending;
			app.source = isDev ? SOURCE_TYPE.Development : SOURCE_TYPE.Market;
			this._updateLocalAppsData(app);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);
			console.log('install app start');
			const response = await installApp(app.name);
			this._handleOperationResponse(app, response);
		},

		async resumeApp(app: AppStoreInfo, isDev: boolean) {
			app.status = APP_STATUS.waiting;
			app.source = isDev ? SOURCE_TYPE.Development : SOURCE_TYPE.Market;
			this._updateLocalAppsData(app);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);
			console.log('install app resuming');
			const response = await resumeApp(app.name, app.cfgType);
			this._handleOperationResponse(app, response);
		},

		async suspendApp(app: AppStoreInfo, isDev: boolean) {
			app.status = APP_STATUS.waiting;
			app.source = isDev ? SOURCE_TYPE.Development : SOURCE_TYPE.Market;
			this._updateLocalAppsData(app);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);
			console.log('install app resuming');
			const response = await suspendApp(app.name, app.cfgType);
			this._handleOperationResponse(app, response);
		},

		async uninstallApp(app: AppStoreInfo, isDev: boolean) {
			app.source = isDev ? SOURCE_TYPE.Development : SOURCE_TYPE.Market;
			app.status = APP_STATUS.uninstalling;
			this._updateLocalAppsData(app);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);
			console.log('uninstall app start');

			const response = await uninstallApp(app.name, app.cfgType);
			this._handleOperationResponse(app, response);
		},

		async upgradeApp(app: AppStoreInfo) {
			app.source = SOURCE_TYPE.Market;
			app.status = APP_STATUS.upgrading;
			this._updateLocalAppsData(app);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);
			console.log('upgrade app start');
			const response = await upgradeApp(app.name);
			this._handleOperationResponse(app, response);
		},

		async cancelInstallingApp(app: AppStoreInfo, isDev: boolean) {
			app.source = isDev ? SOURCE_TYPE.Development : SOURCE_TYPE.Market;
			console.log('cancel app start');
			app.status = APP_STATUS.waiting;
			this._updateLocalAppsData(app);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);
			await cancelInstalling(app.name, app.cfgType);
		},

		updateAppStatusBySocket(
			uid: string,
			operation: string,
			op_status: string,
			progress: string,
			message: string
		) {
			if (!uid || !operation || !op_status) {
				console.log('app update message error');
				return;
			}

			const dealAppStatus = (app: AppStoreInfo) => {
				if (op_status === OPERATE_STATUS.failed) {
					this._appBackendFailure(app, message);
					return;
				}

				switch (app.cfgType) {
					case CFG_TYPE.APPLICATION:
						this._handleAppStatus(app, operation, op_status);
						break;
					case CFG_TYPE.MODEL:
						app.progress = progress ? progress : '0';
						this._handleModelStatus(app, operation, op_status);
						break;
					case CFG_TYPE.WORK_FLOW:
						this._handleWorkFlowStatus(app, operation, op_status);
						break;
				}
			};

			const app = this.installApps.find((item) => item.name == uid);

			if (!app) {
				getApp(uid).then((appInfo) => {
					if (appInfo) {
						const find = this.installApps.find(
							(item) => item.name == appInfo.name
						);
						if (find) {
							console.log('app has been added' + uid);
							return;
						}
						this.installApps.unshift(appInfo);
						dealAppStatus(appInfo);
					} else {
						console.log('get app failure' + uid);
					}
				});
			} else {
				dealAppStatus(app);
			}
		},

		/**
		 *
		 * App Status
		 * +-----------+  install   +---------+           +------------+            +--------------+    suspend     +---------+
		 * | uninstall | --------->| pending | ---------> | installing | ---------> |              | -------------> | suspend |
		 * +-----------+           +---------+           +------------+             |              |               +---------+
		 *       ^                                                                  |              |                    |
		 *       |                                          +---------------------> |   running    |                    | resume
		 *       |                                          |                       |              |                    |
		 *       |                               +------------+      upgrade        |              |                +----------+
		 *       |                               | upgrading  | <------------------ |              |<-------------+ | resuming |
		 *       |                               +------------+                     +--------------+                +----------+
		 *       |                                                                       |
		 *       |                                                                       |  uninstall
		 *       |                                                                       v
		 *       |                                                             +--------------+
		 *       ------------------------------------------------------------- | uninstalling |
		 *                                                                     +--------------+
		 *
		 * Operate Status
		 *                                                cancel
		 *                      +-----------------------------------------------------------+
		 *                      |                                                           v
		 *      install   +----------+     +------------+          cancel             +-----------+
		 *     ---------> | pending  | --> |            | --------------------------> | canceled  |
		 *                +----------+     |            |                             +-----------+
		 *                     ^           |            |     suspend/resume/uninstall
		 *                     | upgrade   | processing | <-----------------------------+
		 *                     |           |            |                               |
		 *                     |           |            |                             +-----------+
		 *                     |           |            | --------------------------> | completed |
		 *                     |           +------------+                             +-----------+
		 *                     |                |                                          |
		 *                     |                |                                          |
		 *                     |                v                                          |
		 *                     |           +------------+                                  |
		 *                     |           |   failed   |                                  |
		 *                     |           +------------+                                  |
		 *                     |                                                           |
		 *                     +-----------------------------------------------------------+
		 */
		async _handleAppStatus(
			app: AppStoreInfo,
			operation: string,
			op_status: string
		) {
			let refresh = false;
			// switch (app.status) {
			// 	case APP_STATUS.uninstalled:
			// 	case APP_STATUS.pending:
			if (
				operation === OPERATE_ACTION.install &&
				op_status === OPERATE_STATUS.processing
			) {
				app.status = APP_STATUS.installing;
			}
			// break;
			// case APP_STATUS.installing:
			if (
				operation === OPERATE_ACTION.install &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.running;
				refresh = true;
			}
			if (
				operation === OPERATE_ACTION.cancel &&
				op_status === OPERATE_STATUS.canceled
			) {
				app.status = APP_STATUS.uninstalled;
				refresh = true;
			}
			// break;
			// case APP_STATUS.running:
			if (
				operation === OPERATE_ACTION.suspend &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.suspend;
			}
			if (
				operation === OPERATE_ACTION.upgrade &&
				op_status === OPERATE_STATUS.processing
			) {
				app.status = APP_STATUS.upgrading;
			}
			if (
				operation === OPERATE_ACTION.uninstall &&
				op_status === OPERATE_STATUS.processing
			) {
				app.status = APP_STATUS.uninstalling;
			}
			// break;
			// case APP_STATUS.upgrading:
			if (
				operation === OPERATE_ACTION.upgrade &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.running;
				refresh = true;
			}
			// break;
			// case APP_STATUS.resuming:
			if (
				operation === OPERATE_ACTION.resume &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.running;
			}
			// break;
			// case APP_STATUS.uninstalling:
			if (
				operation === OPERATE_ACTION.uninstall &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.uninstalled;
				refresh = true;
			}
			// break;
			// }
			this._notificationData(app, refresh);
		},

		/**
		 *  Recommend Status
		 *
		 *   				                upgrade/uninstall
		 *                  +-----------------------------------+
		 *                  v                                   |
		 *      install   +------------+                      +-----------+
		 *     ---------> |            | -------------------> | completed |
		 *                | processing |                      +-----------+
		 *                |            |                        ^
		 *                |            | -----------------------+
		 *                +------------+
		 *                  |
		 *                  |
		 *                  v
		 *                +------------+
		 *                |   failed   |
		 *                +------------+
		 *
		 *  Operate Status
		 *      +-------------------------------------------------+
		 *      v                                                 |
		 *    +-----------+  install   +---------+  uninstall   +--------------+
		 *    | notfound | ---------> | running | -----------> | uninstalling |
		 *    +-----------+            +---------+              +--------------+
		 *
		 */
		async _handleWorkFlowStatus(
			app: AppStoreInfo,
			operation: string,
			op_status: string
		) {
			let refresh = false;
			if (
				operation === OPERATE_ACTION.install &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.running;
			}
			if (
				operation === OPERATE_ACTION.upgrade &&
				op_status === OPERATE_STATUS.processing
			) {
				app.status = APP_STATUS.upgrading;
			}
			if (
				operation === OPERATE_ACTION.upgrade &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.running;
				refresh = true;
			}
			if (
				operation === OPERATE_ACTION.uninstall &&
				op_status === OPERATE_STATUS.processing
			) {
				app.status = APP_STATUS.uninstalling;
			}
			if (
				operation === OPERATE_ACTION.uninstall &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.uninstalled;
				refresh = true;
			}
			this._notificationData(app, refresh);
		},

		/**
		 *
		 * Model Status
		 *                cancel                                             suspend
		 *      +-----------------------------+                   +-------------------------+
		 *      v                             |                   v                         |
		 * +--------------+  install     +------------+     +-----------+  resume    +---------+
		 * | no_installed | -----------> | installing | --> | installed | ---------> | running |
		 * +--------------+              +------------+     +-----------+            +---------+
		 *      ^                            |                  |                        |
		 *      |             uninstall      |                  |                        |
		 *      +-----------------------------------------------+                        |
		 *      |                                                                        |
		 *      |                                     uninstall                          |
		 *      -------------------------------------------------------------------------+
		 *
		 *
		 * Operate Status
		 *
		 *
		 *      install   +------------+  cancel       +------------+
		 *     ---------> |            | ------------> |  canceled  |
		 *                |            |               +------------+
		 *                |            |
		 *                |						 |  resume/suspend/uninstall
		 *                | processing | <----------------------+
		 *                |            |                        |
		 *                |            |               +------------+
		 *                |            | ------------> | completed  |
		 *                +------------+               +------------+
		 *                      |
		 *                      |
		 *                      v
		 *                +------------+
		 *                |   failed   |
		 *                +------------+
		 *
		 **/
		async _handleModelStatus(
			app: AppStoreInfo,
			operation: string,
			op_status: string
		) {
			let refresh = false;
			// switch (app.status) {
			// 	case APP_STATUS.uninstalled:
			if (
				operation === OPERATE_ACTION.install &&
				op_status === OPERATE_STATUS.processing
			) {
				app.status = APP_STATUS.installing;
			}
			// break;
			// case APP_STATUS.installing:
			if (
				operation === OPERATE_ACTION.install &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.installed;
				refresh = true;
			}
			if (
				operation === OPERATE_ACTION.cancel &&
				op_status === OPERATE_STATUS.canceled
			) {
				app.status = APP_STATUS.uninstalled;
				refresh = true;
			}
			// break;
			// case APP_STATUS.installed:
			if (
				operation === OPERATE_ACTION.resume &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.running;
				refresh = true;
			}
			if (
				operation === OPERATE_ACTION.uninstall &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.uninstalled;
				refresh = true;
			}
			// break;
			// case APP_STATUS.running:
			if (
				operation === OPERATE_ACTION.suspend &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.installed;
			}
			if (
				operation === OPERATE_ACTION.uninstall &&
				op_status === OPERATE_STATUS.completed
			) {
				app.status = APP_STATUS.uninstalled;
				refresh = true;
			}
			// break;
			// }

			this._notificationData(app, refresh);
		},

		_notificationData(app: AppStoreInfo, refresh: boolean) {
			this._updateLocalAppsData(app!);
			bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, app);

			if (refresh) {
				const store = useUserStore();
				this.loadApps();
				store.loadLocalResourceData();
			}
		},

		async _getInstalledApps() {
			const newData = await getInstalledApps();
			newData.forEach((newApp) => {
				const index = this.installApps.findIndex(
					(app) => app.name == newApp.name
				);
				if (index >= 0) {
					console.log('find and replace');
					this.installApps.splice(index, 1, newApp);
				} else {
					console.log('not find insert app');
					this.installApps.unshift(newApp);
				}
			});

			this.installApps.sort((before, after) => {
				return (
					new Date(after.installTime ?? 0).getTime() -
					new Date(before.installTime ?? 0).getTime()
				);
			});
		},

		async _getUpdateApps() {
			this.updateApps = this.installApps.filter((app) => app.needUpdate);

			const queryState = [APP_STATUS.upgrading];
			const workingNameList = await getAppsBriefInfoByStatus(queryState);
			if (workingNameList && workingNameList.length > 0) {
				const workingAppList = await getAppsByNames(workingNameList);
				workingAppList.forEach((app) => {
					const index = this.updateApps.findIndex((needUpdateApp) => {
						return needUpdateApp.name === app.name;
					});
					if (index > -1) {
						this.updateApps.splice(index, 1, app);
					} else {
						this.updateApps.unshift(app);
					}
				});
			}
		},

		async _appBackendFailure(app: AppStoreInfo, message: string) {
			bus.emit(BUS_EVENT.APP_BACKEND_ERROR, message);
			//recover_app_status
			getApp(app.name).then((newApp) => {
				if (newApp) {
					app.status = newApp.status;
					app.preflightError = [];
					bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, newApp);
					this._updateLocalAppsData(app);
				}
			});
		},

		async _updateLocalAppsData(app: AppStoreInfo): Promise<void> {
			console.log('update local');
			const index = this.installApps.findIndex(
				(item) => item.name === app.name
			);
			if (index > -1) {
				if (app.status === APP_STATUS.uninstalled) {
					this.installApps.splice(index, 1);
					console.log('install delete ' + app.name);
				} else {
					this.installApps.splice(index, 1, app);
					console.log('install replace ' + app.name);
				}
			} else {
				if (app.status === APP_STATUS.running) {
					this.installApps.unshift(app);
					console.log('install add ' + app.name);
				}
			}

			const index2 = this.updateApps.findIndex(
				(item) => item.name === app.name
			);
			if (index2 > -1) {
				if (app.needUpdate) {
					this.updateApps.splice(index2, 1, app);
					console.log('update replace ' + app.name);
				} else {
					this.updateApps.splice(index2, 1);
					console.log('update delete ' + app.name);
				}
			}

			if (this.pageData) {
				this.pageData.forEach((item: any) => {
					updateCategoryData(item, app);
				});
			}
		},

		_handleOperationResponse(app: AppStoreInfo, response: any): boolean {
			console.log(response);
			if (response && response.code === 200) {
				return true;
			}

			const message =
				response && response.message
					? decodeUnicode(response.message)
					: i18n.global.t('error.operation_preform_failure');
			this._appBackendFailure(app, message);
			return false;
		}
	}
});
