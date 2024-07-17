import {
	APP_STATUS,
	AppStoreInfo,
	TerminusResource,
	UserResource
} from 'src/constants/constants';
import axios from 'axios';
import { TerminusApp } from '@bytetrade/core';

export async function getMyApps(): Promise<TerminusApp[]> {
	try {
		const { data }: any = await axios.get('/app-store/v1/myapps');
		console.log('get_my_apps');
		console.log(data.items);
		return data.items ?? [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getUserInfo() {
	try {
		const response: any = await axios.get('/app-store/v1/user-info');
		console.log('get_user_info');
		console.log(response);
		return response ? response : null;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getOsVersion(): Promise<string | null> {
	try {
		const { version }: any = await axios.get('/app-store/v1/terminus/version');
		console.log('get_os_version');
		console.log(version);
		return version ? version : null;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getInstalledApps(): Promise<AppStoreInfo[]> {
	try {
		const { data }: any = await axios.get('/app-store/v1/myterminus');
		console.log('get_installed_apps');
		console.log(data);
		return data ? data : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getUserResource(): Promise<UserResource | null> {
	try {
		const data: any = await axios.get('/app-store/v1/user/resource');
		console.log('get_user_resource');
		console.log(data);
		return data;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getSystemResource(): Promise<TerminusResource | null> {
	try {
		const data: any = await axios.get('/app-store/v1/cluster/resource');
		console.log('get_system_resource');
		console.log(data);
		return data;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getOperateHistory(source: string): Promise<any> {
	try {
		const { result }: any = await axios.get(
			'/app-store/v1/operate_history?resourceType=app|model|recommend&source=' +
				source
		);
		console.log(result);
		return result ? result : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getAppsBriefInfoByStatus(state: APP_STATUS[]) {
	try {
		const response: any = await axios.get('/app-store/v1/apps/app', {
			params: {
				issysapp: false,
				state: state.join('|')
			}
		});
		console.log('get_apps_brief_info_by_status');
		console.log(response);
		if (response) {
			const nameList = response.map((item: any) => {
				return item.spec.name;
			});
			console.log(nameList);
			return nameList ? nameList : [];
		}
		return [];
	} catch (e) {
		console.log(e);
		return [];
	}
}
