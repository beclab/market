<template>
	<q-layout class="main-layout" view="lHh Lpr lFf" style="overflow: hidden">
		<q-drawer
			v-model="leftDrawerOpen"
			show-if-above
			:style="appStore.isPublic ? 'margin-left: 20px' : ''"
			:bordered="!appStore.isPublic"
			height="100%"
			:width="appStore.isPublic ? 260 : 240"
		>
			<bt-scroll-area
				:style="
					appStore.isPublic ? 'height : 100vh' : 'height: calc(100vh - 65px)'
				"
				@scroll="onScroll"
			>
				<bt-menu
					active-class="my-active-link"
					:items="itemsRef"
					v-model="menuStore.currentItem"
					@select="changeItemMenu"
				>
					<template v-if="appStore.isPublic" v-slot:header>
						<div class="header-bar column justify-start q-px-md q-mb-xs">
							<q-img class="header-icon" src="/icons/market-128*128.png">
								<template v-slot:loading>
									<q-skeleton class="header-icon" style="border-radius: 20px" />
								</template>
							</q-img>
							<span class="text-h5 text-grey-10 q-mt-md">{{
								t('main.terminus_market')
							}}</span>
						</div>
					</template>

					<!--					<template-->
					<!--						v-slot:[`icon-${menu.key}`]-->
					<!--						v-for="menu in itemsRef[0].children"-->
					<!--						:key="menu.key"-->
					<!--					>-->
					<!--						<div class="custom-icon-div">-->
					<!--							<img-->
					<!--								:src="showIconAddress(menu.icon)"-->
					<!--								:alt="menu.label"-->
					<!--								:class="menuStore.currentItem === menu.key ? 'active-icon' : ''"-->
					<!--							/>-->
					<!--						</div>-->
					<!--					</template>-->
				</bt-menu>
			</bt-scroll-area>
			<div
				v-if="!appStore.isPublic"
				class="bottom-menu-root items-center"
				:style="{
					'--showShadow': showBarShadow ? '1px solid #EBEBEB' : 'none'
				}"
			>
				<q-item
					dense
					clickable
					:active="menuStore.currentItem === MENU_TYPE.MyTerminus"
					active-class="my-active-link"
					class="text-ink-2 bottom-menu row justify-start items-center cursor-pointer"
					@click="changeItemMenu({ key: MENU_TYPE.MyTerminus })"
				>
					<q-icon name="sym_r_home" size="20px" />
					<div class="text-body1 q-ml-sm">
						{{ t('main.my_terminus') }}
					</div>
					<div
						v-if="updateCount !== 0"
						class="bottom-menu-update-size text-subtitle3 bg-background-3 text-ink-2"
					>
						{{ updateCount }}
					</div>
				</q-item>
			</div>
		</q-drawer>

		<q-page-container>
			<router-view v-slot="{ Component }">
				<transition
					:name="transitionName"
					:style="`position: absolute;width:  calc(100% - ${leftDrawerOpen ? '240px' : '0px'})`"
				>
					<keep-alive :exclude="keepAliveExclude" max="10">
						<component
							:is="Component"
							style="overflow-y: hidden"
							:key="route.fullPath"
						/>
					</keep-alive>
				</transition>
			</router-view>
		</q-page-container>

		<q-btn
			v-if="appStore.isPublic"
			class="btn-size-md float-btn"
			@click="installOS"
			>{{ t('main.install_terminus_os') }}</q-btn
		>
	</q-layout>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue';
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router';
import { useMenuStore } from 'src/stores/menu';
import { MENU_TYPE, TRANSACTION_PAGE } from '../constants/constants';
import { useSettingStore } from 'src/stores/setting';
import { useAppStore } from 'src/stores/app';
import { useI18n } from 'vue-i18n';

const { t } = useI18n();
const leftDrawerOpen = ref(false);
const menuStore = useMenuStore();
const Router = useRouter();
const route = useRoute();
// const searchTxt = ref('');
const transitionName = ref();
const position = ref(-1);
const settingStore = useSettingStore();
const keepAliveExclude = ref('LogPage');
const appStore = useAppStore();
const updateCount = ref(0);
const showBarShadow = ref(false);

const itemsRef = computed(() => {
	if (menuStore.menuList.length === 0) {
		return [];
	}
	return [
		{
			label: t('base.extensions'),
			key: 'Application',
			children: menuStore.menuList
		}
	];
});

const changeItemMenu = (data: any): void => {
	const type = data.key;
	menuStore.changeItemMenu(type);
	switch (type) {
		case MENU_TYPE.Application.SocialNetwork:
		case MENU_TYPE.Application.Utilities:
		case MENU_TYPE.Application.Productivity:
		case MENU_TYPE.Application.Blockchain:
		case MENU_TYPE.Application.Entertainment:
			Router.push({
				name: 'Category',
				params: {
					categories: type
				}
			});
			break;
		case MENU_TYPE.Application.Recommendation:
			Router.push({ name: 'Recommend' });
			break;
		case MENU_TYPE.Application.Models:
			Router.push({ name: 'Model' });
			break;
		default:
			Router.push({
				name: type
			});
			break;
	}
};

onBeforeRouteUpdate((to, from, next) => {
	transitionName.value = '';
	next();
});

watch(
	() => route.path,
	(to, from) => {
		// console.log(Router.options.history)
		if (Router.options.history.state) {
			if (
				route.name === TRANSACTION_PAGE.App ||
				route.name === TRANSACTION_PAGE.Discover ||
				route.name === TRANSACTION_PAGE.List ||
				route.name === TRANSACTION_PAGE.Preview ||
				route.name === TRANSACTION_PAGE.Version ||
				route.name === TRANSACTION_PAGE.Log ||
				route.name === TRANSACTION_PAGE.Update ||
				from.includes('/app/') ||
				from.includes('/middleware/') ||
				from.includes('/recommend/') ||
				from.includes('/model/') ||
				from.includes('/discover') ||
				from.includes('/list') ||
				from.includes('/preview') ||
				from.includes('/log') ||
				from.includes('/update') ||
				from.includes('/versionHistory')
			) {
				transitionName.value =
					Number(Router.options.history.state.position) >= position.value
						? 'slide-left'
						: 'slide-right';
			} else {
				transitionName.value = '';
			}
			// console.log(`router position ${Router.options.history.state.position}`)
			// console.log(`current position ${position.value}`)
			// console.log(transitionName.value)
			position.value = Number(Router.options.history.state.position);
		}
	}
);

// nsfw restore
// watch(() => {
//   return settingStore.restore
// },(newValue) => {
//   console.log('reload')
//   if (newValue){
//     keepAliveExclude.value = 'LogPage,InstalledPage,HomePage,DiscoverPage,CategoryPage,AppListPage,AppDetailPage'
//   }else {
//     keepAliveExclude.value = 'SearchPage,LogPage';
//   }
// })

watch(
	() => appStore.installApps,
	() => {
		let size = 0;
		appStore.installApps.forEach((app) => {
			if (app.needUpdate) {
				size++;
			}
		});
		updateCount.value = size;
	},
	{
		deep: true,
		immediate: true
	}
);

watch(
	() => {
		return menuStore.currentItem;
	},
	() => {
		if (settingStore.restore) {
			settingStore.restore = false;
		}
	}
);

onMounted(async () => {
	console.log(route);
	updateMenu();
});

const updateMenu = () => {
	switch (route.name) {
		case MENU_TYPE.Application.Home:
		case MENU_TYPE.Application.Recommendation:
		case MENU_TYPE.Application.Models:
			menuStore.changeItemMenu(route.name);
			break;
		case 'Category':
			if (route.params && route.params.categories) {
				menuStore.changeItemMenu(route.params.categories as string);
			}
			break;
		case MENU_TYPE.MyTerminus:
			menuStore.changeItemMenu(route.name);
			break;
	}
};

const onScroll = async (info: any) => {
	showBarShadow.value =
		info.verticalSize > info.verticalContainerSize &&
		info.verticalPercentage !== 1;
};

const installOS = async () => {
	window.open('https://docs.olares.xyz/manual/get-started/', '_blank');
};
</script>

<style lang="scss" scoped>
.header-bar {
	margin-top: 44px;

	.header-icon {
		width: 56px;
		height: 56px;
	}
}

.main-layout {
	position: relative;

	.bottom-menu-root {
		border-top: var(--showShadow);
		padding: 12px 16px;

		.bottom-menu {
			height: 40px;
			border-radius: 8px;
			padding-left: 8px;
			padding-right: 8px;

			.bottom-menu-update-size {
				width: 32px;
				height: 16px;
				text-align: center;
				border-radius: 8px;
				margin-left: 8px;
			}
		}
	}

	.float-btn {
		background: $background-2;
		color: $ink-1;
		position: absolute;
		right: 24px;
		bottom: 64px;
		border: 1px solid $info;
		border-radius: 46px !important;
		text-transform: unset;
		box-shadow: 0 8px 40px 0 #00000033;
	}
}

.main-layout ::v-deep(.my-active-link) {
	color: $blue-default !important;
	background-color: $blue-soft !important;
}
</style>
