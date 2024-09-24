import axios from 'axios';
import globalConfig from 'src/api/config';
import { MenuData } from 'src/constants/constants';

export async function getMenuData(): Promise<MenuData | null> {
	try {
		const { data }: any = await axios.get(
			globalConfig.url + '/app-store/v1/applications/menutypes'
		);
		console.log('getMenuData');
		console.log(data);
		return data ? data : null;
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function getAppMultiLanguage() {
	try {
		const { data }: any = await axios.get(
			globalConfig.url + '/app-store/v1/applications/i18ns'
		);
		console.log('getAppMultiLanguage');
		console.log(data);
		return data ? data : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}
