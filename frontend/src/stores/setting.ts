import { defineStore } from 'pinia';
import { getNsfw, setNsfw } from 'src/api/private/setting';

export const useSettingStore = defineStore('setting', {
	state: () => ({
		restore: false,
		nsfw: false
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
		}
	}
});
