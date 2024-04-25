import { defineStore } from 'pinia';
import { MENU_TYPE } from '../constants/constants';

export const useMenuStore = defineStore('menu', {
	state: () => {
		return {
			currentItem: MENU_TYPE.Application.Home,
			searchData: '',
			leftDrawerOpen: true
		};
	},
	getters: {
		//doubleCount: (state) => state.counter * 2,
	},
	actions: {
		changeItemMenu(item: string) {
			this.currentItem = item;
		},
		toggleLeftDrawer(status: boolean) {
			this.leftDrawerOpen = status;
		},
		inputSearchData(data: string) {
			this.searchData = data;
		}
	}
});
