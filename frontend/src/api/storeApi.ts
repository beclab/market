import axios from 'axios';
import { AppStoreInfo } from 'src/constants/constants';
import globalConfig from 'src/api/config';

export async function getAppsByNames(names: []): Promise<AppStoreInfo[]> {
	try {
		const { data }: any = await axios.post(
			globalConfig.url + '/app-store/v1/applications/infos',
			names
		);
		console.log('get_apps_by_names');
		console.log(data);
		return data ? data : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getMarkdown(name: string) {
	try {
		const response: any = await axios.get(
			globalConfig.url + '/app-store/v1/readme/' + name
		);
		console.log('getMarkdown');
		if (response.code && response.code === 200) {
			return response.message;
		}
		return null;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getApp(name: string): Promise<AppStoreInfo | null> {
	try {
		const { data }: any = await axios.get(
			globalConfig.url + '/app-store/v1/applications/info/' + name
		);
		console.log('get_app');
		return data;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getVersionHistory(names: string) {
	try {
		const { data }: any = await axios.get(
			globalConfig.url + '/app-store/v1/applications/version-history/' + names
		);
		console.log('get_version_history');
		console.log(data);
		return data ? data : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getPage() {
	try {
		const { data }: any = await axios.get(
			globalConfig.url + '/app-store/v1/pages/detail'
		);
		console.log('get_page');
		console.log(data);
		return data ? data : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getRecommendApps() {
	try {
		const { data }: any = await axios.get(
			globalConfig.url + '/app-store/v1/workflow/recommend/detail'
			// {
			// 	params: {
			// 		category
			// 	}
			// }
		);
		console.log('get_recommend_apps');
		console.log(data);
		return data.items ? data.items : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getTop(category?: string): Promise<AppStoreInfo[]> {
	try {
		let url = '/app-store/v1/applications/top';
		if (category) {
			url = url + '?category=' + category;
		}
		const { data }: any = await axios.get(globalConfig.url + url);
		console.log('get_top');
		console.log(data.items);
		return data.items ?? [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getTypes(): Promise<AppStoreInfo[]> {
	try {
		let url = '/app-store/v1/applications/menutypes';
		const { data }: any = await axios.get(globalConfig.url + url);
		console.log('get_types');
		console.log(data.items);
		return data.items ?? [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function getLatest(category?: string): Promise<AppStoreInfo[]> {
	try {
		getTypes()
		
		let url = '/app-store/v1/applications/latest';
		if (category) {
			url = url + '?category=' + category;
		}
		const { data }: any = await axios.get(globalConfig.url + url);
		console.log('get_latest');
		console.log(data.items);
		return data.items ?? [];
	} catch (e) {
		console.log(e);
		return [];
	}
}

export async function searchApplications(q?: string) {
	try {
		const { data }: any = await axios.post(
			globalConfig.url + '/app-store/v1/applications/search',
			{
				name: q
			}
		);

		console.log('search_applications');
		console.log(data.items);
		return data.items ?? [];
	} catch (e) {
		console.log(e);
		return [];
	}
}
