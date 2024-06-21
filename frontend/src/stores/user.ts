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
import { intersection, Range, Version } from 'src/utils/utils';
import {
	getMyApps,
	getOsVersion,
	getSystemResource,
	getUserInfo,
	getUserResource
} from 'src/api/private/user';
import { i18n } from 'src/boot/i18n';
import { TerminusApp } from '@bytetrade/core';
import { CFG_TYPE } from 'src/constants/config';
import { bus, BUS_EVENT } from 'src/utils/bus';

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
			if (
				role &&
				terminus &&
				userResource &&
				systemResource &&
				appDependencies
			) {
				app.status = status;
			} else {
				app.status = APP_STATUS.preflightFailed;
				if (app.preflightError.length == 0) {
					app.preflightError.push(i18n.global.t('error.unknown_error'));
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
				app.preflightError.push(i18n.global.t('error.failed_get_user_role'));
				return false;
			}

			if (app.onlyAdmin && this.user.role !== ROLE_TYPE.Admin) {
				app.preflightError.push(
					i18n.global.t('error.only_be_installed_by_the_admin')
				);
				return false;
			}
			if (
				app.cfgType === CFG_TYPE.MIDDLEWARE &&
				this.user.role !== ROLE_TYPE.Admin
			) {
				app.preflightError.push(
					i18n.global.t('error.not_admin_role_install_middleware')
				);
				return false;
			}
			if (
				app.options &&
				app.options.appScope &&
				app.options.appScope.clusterScoped &&
				this.user.role !== ROLE_TYPE.Admin
			) {
				app.preflightError.push(
					i18n.global.t('error.not_admin_role_install_cluster_app')
				);
				return false;
			}
			return true;
		},

		_terminusOSVersionPreflight(app: AppStoreInfo): boolean {
			if (!this.osVersion) {
				app.preflightError.push(
					i18n.global.t('error.failed_to_get_os_version')
				);
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
							app.preflightError.push(
								i18n.global.t('error.app_is_not_compatible_terminus_os')
							);
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
							app.preflightError.push(
								i18n.global.t('error.app_is_not_compatible_terminus_os')
							);
							return false;
						}
					}
				}
			}
			app.preflightError.push(
				i18n.global.t('error.app_is_not_compatible_terminus_os')
			);
			return false;
		},

		/**
		 * The user resources are only checked for CPU and memory, where a total value of 0 in the return indicates no limit.
		 * @param app
		 */
		_userResourcePreflight(app: AppStoreInfo): boolean {
			if (!this.userResource) {
				app.preflightError.push(
					i18n.global.t('error.failed_to_get_user_resource')
				);
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
				app.preflightError.push(i18n.global.t('error.user_not_enough_cpu'));
				isOK = false;
			}

			const availableMemory =
				this.userResource.memory.total - this.userResource.memory.usage;
			if (
				app.requiredMemory &&
				this.userResource.memory.total &&
				Number(app.requiredMemory) > availableMemory
			) {
				app.preflightError.push(i18n.global.t('error.user_not_enough_memory'));
				isOK = false;
			}
			return isOK;
		},

		_appDependenciesPreflight(app: AppStoreInfo): boolean {
			if (
				app.options &&
				app.options.dependencies &&
				app.options.dependencies.length > 0
			) {
				const nameList = this.myApps.map((item) => item.name);
				if (
					this.systemResource &&
					this.systemResource.apps &&
					this.systemResource.apps.length > 0
				) {
					this.systemResource.apps.forEach((app: Dependency) => {
						nameList.push(app.name);
					});
				}

				for (let i = 0; i < app.options.dependencies.length; i++) {
					const dependency = app.options.dependencies[i];
					if (
						dependency.type === DEPENDENCIES_TYPE.application ||
						dependency.type === DEPENDENCIES_TYPE.middleware
					) {
						this._saveDependencies(app, dependency);

						if (!nameList.includes(dependency.name)) {
							app.preflightError.push(
								i18n.global.t('error.need_to_install_dependent_app_first')
							);
							return false;
						}
					}
				}
			}
			return true;
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
				app.preflightError.push(
					i18n.global.t('error.failed_to_get_system_resource')
				);
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
				app.preflightError.push(i18n.global.t('error.terminus_not_enough_cpu'));
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
				app.preflightError.push(
					i18n.global.t('error.terminus_not_enough_memory')
				);
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
				app.preflightError.push(
					i18n.global.t('error.terminus_not_enough_disk')
				);
				isOK = false;
			}

			const availableGpu = this.systemResource.metrics.gpu.total > 0;
			if (app.requiredGpu && Number(app.requiredGpu) && !availableGpu) {
				app.preflightError.push(i18n.global.t('error.terminus_not_enough_gpu'));
				isOK = false;
			}

			if (
				!app.supportArch ||
				app.supportArch.length === 0 ||
				!this.systemResource.nodes ||
				this.systemResource.nodes.length === 0
			) {
				app.preflightError.push(i18n.global.t('error.terminus_not_enough_gpu'));
				isOK = false;
			} else {
				const intersectedArray = intersection(
					this.systemResource.nodes,
					app.supportArch
				);
				if (intersectedArray.length === 0) {
					app.preflightError.push(
						i18n.global.t('error.cluster_not_support_platform')
					);
					isOK = false;
				}
			}
			return isOK;
		}
	}
});
