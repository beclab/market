import { AppStoreInfo } from 'src/constants/constants';
import { getAppsByNames } from 'src/api/storeApi';
import { bus, BUS_EVENT } from 'src/utils/bus';

export function initCategoryStatus(categoryData: any) {
	console.log(categoryData);
	categoryData.data.forEach((modelData: any) => {
		if (modelData.type === 'Recommends') {
			console.log('Recommend');
			const appList = modelData.content;
			if (appList && appList.length > 0) {
				const appStatus = (appList[0] as AppStoreInfo).status;
				console.log(appStatus);
				if (!appStatus) {
					const workingNameList = appList.map((item: any) => item.name);
					console.log(workingNameList);
					getAppsByNames(workingNameList).then((data) => {
						console.log('Recommend new data');
						console.log(data);
						modelData.content = data;
						data.forEach((item: AppStoreInfo) => {
							bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, item);
						});
					});
				}
			}
		}
		if (modelData.type === 'Topic') {
			console.log('Topic');
			const appList = modelData.content;
			if (appList && appList.length > 0) {
				appList.forEach((item: any) => {
					if (item.apps.length > 0) {
						const workingNameList = item.apps.map((item: any) => item.name);
						console.log(workingNameList);
						getAppsByNames(workingNameList).then((data) => {
							console.log('Topic new data');
							console.log(data);
							item.apps = data;
							data.forEach((item: AppStoreInfo) => {
								bus.emit(BUS_EVENT.UPDATE_APP_STORE_INFO, item);
							});
						});
					}
				});
			}
		}
	});
}

export function updateCategoryData(categoryData: any, app: AppStoreInfo) {
	if (!categoryData.data) {
		console.log('skip');
		return;
	}

	categoryData.data.forEach((modelData: any) => {
		if (modelData.type === 'Recommends') {
			const appList = modelData.content;
			if (appList && appList.length > 0) {
				const appStatus = (appList[0] as AppStoreInfo).status;
				if (!appStatus) {
					const appIndex = appList.findIndex(
						(item: any) => app.name === item.name
					);
					if (appIndex >= 0) {
						modelData.content.splice(appIndex, 1, app);
					}
				}
			}
		}
		if (modelData.type === 'Topic') {
			console.log('Topic');
			const appList = modelData.content;
			if (appList && appList.length > 0) {
				appList.forEach((item: any) => {
					if (item.apps.length > 0) {
						const appIndex = item.apps.findIndex(
							(item: any) => app.name === item.name
						);
						if (appIndex >= 0) {
							item.apps.splice(appIndex, 1, app);
						}
					}
				});
			}
		}
	});
}
