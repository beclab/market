import { APP_STATUS, AppStoreInfo } from 'src/constants/constants';

export enum CFG_TYPE {
	APPLICATION = 'app',
	WORK_FLOW = 'recommend',
	MODEL = 'model',
	MIDDLEWARE = 'middleware'
}

export function showIcon(cfgType: string): boolean {
	return cfgType === CFG_TYPE.MIDDLEWARE || cfgType === CFG_TYPE.APPLICATION;
}

export function canInstallingCancel(cfgType: string): boolean {
	return cfgType !== CFG_TYPE.WORK_FLOW;
}

export function canOpen(cfgType: string): boolean {
	return cfgType === CFG_TYPE.APPLICATION;
}

export function requiredPermissions(cfgType: string): boolean {
	return cfgType === CFG_TYPE.APPLICATION;
}

export function canUnload(app: AppStoreInfo): boolean {
	return app.cfgType === CFG_TYPE.MODEL && app.status == APP_STATUS.running;
}

export function canLoad(app: AppStoreInfo): boolean {
	return app.cfgType === CFG_TYPE.MODEL && app.status === APP_STATUS.installed;
}

export function showDownloadProgress(app: AppStoreInfo): boolean {
	return (
		(app.status === APP_STATUS.installing && app.cfgType === CFG_TYPE.MODEL) ||
		(app.status === APP_STATUS.downloading &&
			app.cfgType === CFG_TYPE.APPLICATION)
	);
}

export function showAppStatus(app: AppStoreInfo): boolean {
	return (
		app.status === APP_STATUS.uninstalling ||
		app.status === APP_STATUS.upgrading ||
		(app.status === APP_STATUS.installing && app.cfgType !== CFG_TYPE.MODEL)
	);
}
