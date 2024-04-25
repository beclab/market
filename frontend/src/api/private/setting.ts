import axios from 'axios';
export async function setNsfw(nsfw: boolean): Promise<boolean> {
	try {
		const { data }: any = await axios.post('/app-store/v1/settings/nsfw', {
			nsfw: nsfw
		});
		console.log(data);
		return true;
	} catch (e) {
		console.log(e);
		return false;
	}
}

export async function getNsfw(): Promise<boolean> {
	try {
		const { data }: any = await axios.get('/app-store/v1/settings/nsfw', {});
		console.log(data);
		return data.nsfw;
	} catch (e) {
		console.log(e);
		return false;
	}
}
