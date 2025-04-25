import { defineStore } from 'pinia';
import { getNsfw, setNsfw } from 'src/api/private/setting';
import {
	AppStoreInfo,
	ClusterApp,
	DEPENDENCIES_TYPE
} from 'src/constants/constants';
import { useUserStore } from 'src/stores/user';

export const useSettingStore = defineStore('setting', {
	state: () => ({
		restore: false,
		nsfw: false,
		dependencyShow: false,
		referenceShow: false
	}),

	actions: {
		async init() {
			this.nsfw = await getNsfw();
		},
		async setNsfw(status: boolean) {
			const result = await setNsfw(status);
			if (result) {
				this.nsfw = status;
				this.setRestore(true);
			}
		},
		setRestore(restore: boolean) {
			this.restore = restore;
		},
		hasDependency(app: AppStoreInfo) {
			if (this.dependencyShow) {
				return false;
			}
			if (this.referenceShow) {
				return false;
			}
			if (
				app?.options &&
				app?.options.dependencies &&
				app?.options.dependencies.filter(
					(item) =>
						item.type === DEPENDENCIES_TYPE.middleware ||
						item.type === DEPENDENCIES_TYPE.application
				).length > 0
			) {
				const userStore = useUserStore();
				const nameList = userStore.myApps.map((item) => item.name);
				if (userStore.systemResource) {
					userStore.systemResource.apps.forEach((app: ClusterApp) => {
						nameList.push(app.name);
					});
				}

				for (let i = 0; i < app.options.dependencies.length; i++) {
					const dependency = app.options.dependencies[i];
					if (
						dependency.type === DEPENDENCIES_TYPE.middleware ||
						dependency.type === DEPENDENCIES_TYPE.application
					) {
						if (!nameList.includes(dependency.name)) {
							return true;
						}
					}
				}
			}
			return false;
		},
		hasReference(app: AppStoreInfo) {
			if (this.dependencyShow) {
				return false;
			}
			if (this.referenceShow) {
				return false;
			}
			if (
				app?.options &&
				app?.options.appScope &&
				app?.options.appScope.appRef &&
				app?.options.appScope.appRef.length > 0
			) {
				const userStore = useUserStore();
				const nameList = userStore.myApps.map((item) => item.name);
				if (userStore.systemResource) {
					userStore.systemResource.apps.forEach((app: ClusterApp) => {
						nameList.push(app.name);
					});
				}

				console.log(nameList);

				for (let i = 0; i < app.options.appScope.appRef.length; i++) {
					if (!nameList.includes(app.options.appScope.appRef[i])) {
						return true;
					}
				}
			}

			return false;
		}
	}
});
