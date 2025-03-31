import { defineStore } from 'pinia';
import {
	APP_STATUS,
	AppStoreInfo,
	DEPENDENCIES_TYPE,
	Dependency,
	ROLE_TYPE,
	TerminusResource,
	User,
	UserResource
} from 'src/constants/constants';
import { intersection } from 'src/utils/utils';
import {
	getMyApps,
	getOsVersion,
	getSystemResource,
	getUserInfo,
	getUserResource
} from 'src/api/private/user';
import { TerminusApp } from '@bytetrade/core';
import { CFG_TYPE } from 'src/constants/config';
import { bus, BUS_EVENT } from 'src/utils/bus';
import { Range, Version } from 'src/utils/version';
import { appPushError, ErrorCode } from 'src/constants/errorCode';

export type UserState = {
	userResource: UserResource | null;
	systemResource: TerminusResource | null;
	user: User | null;
	initialized: boolean;
	myApps: TerminusApp[];
	osVersion: string | null;
	dependencies: Record<string, string[]>;
};

export const useUserStore = defineStore('userStore', {
	state: () => {
		return {
			userResource: null,
			systemResource: null,
			user: null,
			initialized: false,
			myApps: [],
			osVersion: null,
			dependencies: {}
		} as UserState;
	},

	actions: {
		async init() {
			console.log('user init');
			await Promise.all([
				this._getUserInfo(),
				this._getOsVersion(),
				this._getMyApps(),
				this._getUserResource(),
				this._getSystemResource()
			]);
			console.log('user init requests completed');
			this.initialized = true;
		},

		async loadLocalResourceData() {
			console.log('update local data');
			await Promise.all([
				this._getMyApps(),
				this._getUserResource(),
				this._getSystemResource()
			]);
			console.log('local data requests completed');
		},

		frontendPreflight(
			app: AppStoreInfo,
			status: APP_STATUS = APP_STATUS.installable
		) {
			app.preflightError = [];
			const role = this._userRolePreflight(app);
			const terminus = this._terminusOSVersionPreflight(app);
			const userResource = this._userResourcePreflight(app);
			const systemResource = this._systemResourcePreflight(app);
			const appDependencies = this._appDependenciesPreflight(app);
			const appConflicts = this._appConflictsPreflight(app);
			if (
				role &&
				terminus &&
				userResource &&
				systemResource &&
				appDependencies &&
				appConflicts
			) {
				app.status = status;
			} else {
				app.status = APP_STATUS.preflightFailed;
				if (app.preflightError.length == 0) {
					appPushError(app, ErrorCode.G001);
				}
			}
		},
		async _getUserInfo() {
			this.user = await getUserInfo();
		},
		async _getOsVersion() {
			this.osVersion = await getOsVersion();
		},
		async _getMyApps() {
			this.myApps = await getMyApps();
		},
		async _getUserResource() {
			this.userResource = await getUserResource();
		},
		async _getSystemResource() {
			this.systemResource = await getSystemResource();
		},
		_userRolePreflight(app: AppStoreInfo): boolean {
			if (!this.user || !this.user.role) {
				appPushError(app, ErrorCode.G003);
				return false;
			}

			if (app.onlyAdmin && this.user.role !== ROLE_TYPE.Admin) {
				appPushError(app, ErrorCode.G004);
				return false;
			}
			if (
				app.cfgType === CFG_TYPE.MIDDLEWARE &&
				this.user.role !== ROLE_TYPE.Admin
			) {
				appPushError(app, ErrorCode.G005);
				return false;
			}
			if (
				app.options &&
				app.options.appScope &&
				app.options.appScope.clusterScoped &&
				this.user.role !== ROLE_TYPE.Admin
			) {
				appPushError(app, ErrorCode.G006);
				return false;
			}
			return true;
		},

		_terminusOSVersionPreflight(app: AppStoreInfo): boolean {
			if (!this.osVersion) {
				appPushError(app, ErrorCode.G007);
				return false;
			}
			if (
				app.options &&
				app.options.dependencies &&
				app.options.dependencies.length > 0
			) {
				for (let i = 0; i < app.options.dependencies.length; i++) {
					const appInfo = app.options.dependencies[i];
					if (appInfo.type === DEPENDENCIES_TYPE.system) {
						//temp
						if (appInfo.version == '>=0.5.0-0') {
							// console.log('intercept by temporary version : >=0.5.0-0');
							appPushError(app, ErrorCode.G008);
							return false;
						}

						try {
							const range = new Range(appInfo.version);
							if (range) {
								const result = range.satisfies(new Version(this.osVersion));
								// console.log(
								// 	'version satisfies : ' +
								// 		result +
								// 		' version : ' +
								// 		this.osVersion +
								// 		' range : ' +
								// 		appInfo.version
								// );
								if (result) {
									return true;
								}
							}
						} catch (e) {
							console.log(e);
							appPushError(app, ErrorCode.G008);
							return false;
						}
					}
				}
			}
			appPushError(app, ErrorCode.G008);
			return false;
		},

		/**
		 * The user resources are only checked for CPU and memory, where a total value of 0 in the return indicates no limit.
		 * @param app
		 */
		_userResourcePreflight(app: AppStoreInfo): boolean {
			if (
				!this.userResource ||
				!this.userResource.cpu ||
				!this.userResource.memory
			) {
				appPushError(app, ErrorCode.G009);
				return false;
			}

			let isOK = true;

			const availableCpu =
				this.userResource.cpu.total - this.userResource.cpu.usage;
			if (
				app.requiredCpu &&
				this.userResource.cpu.total &&
				Number(app.requiredCpu) > availableCpu
			) {
				appPushError(app, ErrorCode.G014);
				isOK = false;
			}

			const availableMemory =
				this.userResource.memory.total - this.userResource.memory.usage;
			if (
				app.requiredMemory &&
				this.userResource.memory.total &&
				Number(app.requiredMemory) > availableMemory
			) {
				appPushError(app, ErrorCode.G015);
				isOK = false;
			}
			return isOK;
		},

		_appDependenciesPreflight(app: AppStoreInfo): boolean {
			let isOK = true;
			if (
				app.options &&
				app.options.dependencies &&
				app.options.dependencies.length > 0
			) {
				const allAppList = this.myApps.map((item) => item.name);
				const clusterAppAndMiddlewareList: string[] = [];
				if (
					this.systemResource &&
					this.systemResource.apps &&
					this.systemResource.apps.length > 0
				) {
					this.systemResource.apps.forEach((app: Dependency) => {
						allAppList.push(app.name);
						clusterAppAndMiddlewareList.push(app.name);
					});
				}

				for (let i = 0; i < app.options.dependencies.length; i++) {
					const dependency = app.options.dependencies[i];
					if (dependency.type === DEPENDENCIES_TYPE.middleware) {
						this._saveDependencies(app, dependency);

						if (!clusterAppAndMiddlewareList.includes(dependency.name)) {
							appPushError(app, ErrorCode.G012_SG001, {
								name: dependency.name,
								version: dependency.version
							});
							isOK = false;
						}
					}

					if (
						dependency.type === DEPENDENCIES_TYPE.application &&
						dependency.mandatory
					) {
						this._saveDependencies(app, dependency);

						if (!allAppList.includes(dependency.name)) {
							// temp dify dependency dify(for cluster in this.systemResource.apps)
							if (dependency.name === app.name) {
								appPushError(app, ErrorCode.G006);
								isOK = false;
							} else {
								appPushError(app, ErrorCode.G012_SG002, {
									name: dependency.name,
									version: dependency.version
								});
								isOK = false;
							}
						}
					}
				}
			}
			return isOK;
		},

		_saveDependencies(app: AppStoreInfo, dependency: Dependency) {
			const list = this.dependencies[dependency.name];
			if (list) {
				const find = list.find((item) => item === app.name);
				if (!find) {
					list.push(app.name);
				}
			} else {
				this.dependencies[dependency.name] = [app.name];
			}
		},

		notifyDependencies(app: AppStoreInfo) {
			const list = this.dependencies[app.name];
			if (list) {
				list.forEach((item) => {
					bus.emit(BUS_EVENT.UPDATE_APP_DEPENDENCIES, item);
				});
			}
		},

		_systemResourcePreflight(app: AppStoreInfo): boolean {
			if (
				!this.systemResource ||
				!this.systemResource.metrics ||
				!this.systemResource.nodes
			) {
				appPushError(app, ErrorCode.G013);
				return false;
			}

			let isOK = true;

			const availableCpu =
				this.systemResource.metrics.cpu.total -
				this.systemResource.metrics.cpu.usage;
			if (
				app.requiredCpu &&
				this.systemResource.metrics.cpu.total &&
				Number(app.requiredCpu) > availableCpu
			) {
				appPushError(app, ErrorCode.G014);
				isOK = false;
			}

			const availableMemory =
				this.systemResource.metrics.memory.total -
				this.systemResource.metrics.memory.usage;
			if (
				app.requiredMemory &&
				this.systemResource.metrics.memory.total &&
				Number(app.requiredMemory) > availableMemory
			) {
				appPushError(app, ErrorCode.G015);
				isOK = false;
			}

			const availableDisk =
				this.systemResource.metrics.disk.total -
				this.systemResource.metrics.disk.usage;
			if (
				app.requiredDisk &&
				this.systemResource.metrics.disk.total &&
				Number(app.requiredDisk) > availableDisk
			) {
				appPushError(app, ErrorCode.G016);
				isOK = false;
			}

			const availableGpu = this.systemResource.metrics.gpu.total > 0;
			if (app.requiredGpu && Number(app.requiredGpu) && !availableGpu) {
				appPushError(app, ErrorCode.G017);
				isOK = false;
			}

			if (
				!app.supportArch ||
				app.supportArch.length === 0 ||
				!this.systemResource.nodes ||
				this.systemResource.nodes.length === 0
			) {
				appPushError(app, ErrorCode.G017);
				isOK = false;
			} else {
				const intersectedArray = intersection(
					this.systemResource.nodes,
					app.supportArch
				);
				if (intersectedArray.length === 0) {
					appPushError(app, ErrorCode.G018);
					isOK = false;
				}
			}
			return isOK;
		},

		_appConflictsPreflight(app: AppStoreInfo): boolean {
			let isOK = true;
			if (
				app.options &&
				app.options.conflicts &&
				app.options.conflicts.length > 0
			) {
				const nameList2 = this.myApps.map((item) => item.name);

				for (let i = 0; i < app.options.conflicts.length; i++) {
					const conflict = app.options.conflicts[i];

					if (conflict.type === DEPENDENCIES_TYPE.application) {
						if (!nameList2.includes(conflict.name)) {
							appPushError(app, ErrorCode.G012_SG001, {
								name: conflict.name
							});
							isOK = false;
						}
					}
				}
			}
			return isOK;
		}
	}
});
