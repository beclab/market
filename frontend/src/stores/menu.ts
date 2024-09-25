import { defineStore } from 'pinia';
import { MENU_TYPE, MenuType } from '../constants/constants';
import { getMenuData } from 'src/api/language';
import { i18n } from 'src/boot/i18n';

export type MenuState = {
	menuList: MenuType[];
	currentItem: string;
	searchData: string;
	leftDrawerOpen: boolean;
};

export const useMenuStore = defineStore('menu', {
	state: () => {
		return {
			menuList: [],
			currentItem: MENU_TYPE.Application.Home,
			searchData: '',
			leftDrawerOpen: true
		} as MenuState;
	},
	getters: {
		//doubleCount: (state) => state.counter * 2,
	},
	actions: {
		async init() {
			const result = await getMenuData();
			if (!result) {
				console.log('get menu data error');
				return;
			}
			const languages = result.i18n;
			const languageMap = {};

			for (const lang in languages) {
				if (Object.prototype.hasOwnProperty.call(languages, lang)) {
					const data = languages[lang];
					const resultMap = {};

					for (const key in data) {
						if (Object.prototype.hasOwnProperty.call(data, key)) {
							resultMap[key] = data[key];
						}
					}

					languageMap[lang] = resultMap;
				}
			}

			for (const language in languageMap) {
				i18n.global.mergeLocaleMessage(language, languageMap[language]);
				console.log(
					`========>>>>>> Merged messages for locale ${language}:`,
					i18n.global.getLocaleMessage(language)
				);
			}

			this.menuList = result.menuTypes.map((item) => {
				return {
					label: i18n.global.t(item.label),
					key: item.key,
					icon: item.icon
				};
			});
		},
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
