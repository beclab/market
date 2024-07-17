import { CFG_TYPE } from 'src/constants/config';
import axios from 'axios';

export async function installApp(app_name: string): Promise<any> {
	try {
		return await axios.post(
			'/app-store/v1/applications/' + app_name + '/install',
			{}
		);
	} catch (e) {
		console.log(e);
		return null;
	}
}

export async function resumeApp(
	app_name: string,
	type: CFG_TYPE
): Promise<any> {
	try {
		return await axios.post('/app-store/v1/resume/' + type + '/' + app_name);
	} catch (e) {
		console.log(e);
		return '';
	}
}

export async function suspendApp(
	app_name: string,
	type: CFG_TYPE
): Promise<any> {
	try {
		return await axios.post('/app-store/v1/suspend/' + type + '/' + app_name);
	} catch (e) {
		console.log(e);
		return '';
	}
}

export async function uninstallApp(
	app_name: string,
	type: CFG_TYPE
): Promise<any> {
	try {
		return await axios.post('/app-store/v1/uninstall/' + type + '/' + app_name);
	} catch (e) {
		console.log(e);
		return '';
	}
}

export async function upgradeApp(app_name: string): Promise<any> {
	try {
		return await axios.post(
			`/app-store/v1/applications/${app_name}/upgrade`,
			{}
		);
	} catch (e) {
		console.log(e);
		return '';
	}
}

export async function cancelInstalling(
	app_name: string,
	type: CFG_TYPE
): Promise<boolean> {
	try {
		const { data }: any = await axios.post(
			'/app-store/v1/cancel/' + type + '/' + app_name
		);
		console.log(data);
		return true;
	} catch (e) {
		console.log(e);
		return false;
	}
}

export async function openApplication(app_uid: string): Promise<boolean> {
	try {
		const { data }: any = await axios.post(
			'/app-store/v1/applications/open?id=' + app_uid,
			{}
		);
		console.log(data);
		return true;
	} catch (e) {
		console.log(e);
		return false;
	}
}

export async function uploadDevFile(
	file: any
): Promise<{ name: string; message: string }> {
	try {
		const formData = new FormData();
		formData.append('name', file);
		const response: any = await axios.post(
			'/app-store/v1/applications/dev-upload',
			formData,
			{
				headers: { 'Content-Type': 'multipart/form-data' }
			}
		);
		console.log(response);
		if (response.data) {
			return { name: response.data.uid, message: '' };
		} else {
			return { name: '', message: response.message };
		}
	} catch (e: any) {
		console.log(e);
		return { name: '', message: e.message };
	}
}
