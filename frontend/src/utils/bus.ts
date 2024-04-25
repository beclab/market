import { EventBus } from 'quasar';
import { AppStoreInfo } from 'src/constants/constants';

export const bus = new EventBus();

export enum BUS_EVENT {
	UPDATE_APP_STORE_INFO = 'update_app_store_info',
	APP_BACKEND_ERROR = 'app_backend_error'
}

export function updateAppStoreList(list: AppStoreInfo[], app: AppStoreInfo) {
	const index = list.findIndex((item) => item.name === app.name);
	if (index >= 0) {
		list[index].status = app.status;
		list[index].uid = app.uid;
		if (app.progress) {
			list[index].progress = app.progress;
		}
	}
}
