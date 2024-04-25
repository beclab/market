import { AppStoreInfo } from 'src/constants/constants';
import axios from 'axios';
import globalConfig from 'src/api/config';

export async function getModels(): Promise<AppStoreInfo[]> {
	try {
		const { data } = await axios.get(
			globalConfig.url + '/app-store/v1/model/detail'
		);
		console.log('get_model_list', data);
		return data && data.items ? data.items : [];
	} catch (e) {
		console.log(e);
		return [];
	}
}
