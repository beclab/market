<template>
	<page-container
		:vertical-position="56"
		v-model="showShadow"
		:hide-gradient="true"
		:title-height="56"
	>
		<template v-slot:title>
			<title-bar
				:show-back="true"
				:show="true"
				:title="
					category === CATEGORIES_TYPE.LOCAL.ALL
						? t(
								type === CATEGORIES_TYPE.LOCAL.LATEST
									? 'latest_app_on_terminus'
									: 'top_app_on_terminus'
							)
						: t(
								type === CATEGORIES_TYPE.LOCAL.LATEST
									? 'latest_app_in'
									: 'top_app_in',
								{
									category: getTempI18nValue(category, true)
								}
							)
				"
				:show-title="showShadow"
				:shadow="showShadow"
				@onReturn="router.back()"
			/>
		</template>
		<template v-slot:page>
			<empty-view v-if="isEmpty" :show-title="false" class="empty_view" />
			<div class="list-page" v-else>
				<div class="app-store-application" v-if="loading">
					<template v-for="item in 20" :key="item">
						<app-card :skeleton="true" />
						<app-card-hide-border />
					</template>
				</div>
				<div class="app-store-application" v-else>
					<template v-for="item in applications" :key="item.name">
						<app-card :item="item" />
					</template>
					<app-card-hide-border />
				</div>
			</div>
		</template>
	</page-container>
</template>
<script lang="ts" setup>
import { ref, onBeforeUnmount, onMounted, watch } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import {
	AppStoreInfo,
	CATEGORIES_TYPE,
	getTempI18nValue
} from 'src/constants/constants';
import EmptyView from 'components/base/EmptyView.vue';
import TitleBar from 'components/base/TitleBar.vue';
import { getLatest, getTop } from 'src/api/storeApi';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';
import PageContainer from 'src/components/base/PageContainer.vue';
import AppCard from 'components/appcard/AppCard.vue';
import AppCardHideBorder from 'components/appcard/AppCardHideBorder.vue';
import { useAppStore } from 'src/stores/app';
import { useI18n } from 'vue-i18n';

const showShadow = ref(false);
const router = useRouter();
const route = useRoute();
const applications = ref<AppStoreInfo[]>([]);
const loading = ref(false);
const isEmpty = ref(false);
const appStore = useAppStore();
const { t } = useI18n();
const category = ref();
const type = ref();

const updateApp = (app: AppStoreInfo) => {
	console.log(`list update status ${app.name}`);
	updateAppStoreList(applications.value, app);
};

onMounted(async () => {
	bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

const fetchData = async () => {
	loading.value = true;
	console.log(route.params);
	category.value = route.params.categories as string;
	type.value = route.params.type as string;
	if (!category.value) {
		bus.emit(BUS_EVENT.APP_BACKEND_ERROR, 'category error');
		return;
	}
	let request = category.value.toLowerCase();
	if (category.value === CATEGORIES_TYPE.LOCAL.ALL) {
		request = '';
	}
	if (type.value === CATEGORIES_TYPE.LOCAL.TOP) {
		applications.value = await getTop(request);
	} else if (type.value === CATEGORIES_TYPE.LOCAL.LATEST) {
		applications.value = await getLatest(request);
	} else if (type.value === CATEGORIES_TYPE.LOCAL.RECOMMENDS) {
		const categoryData = await appStore.getPageData(category.value);
		if (categoryData && categoryData.data) {
			console.log(categoryData.data);
			categoryData.data.forEach((item: any) => {
				if (item.type === 'Recommends') {
					applications.value = item.content;
				}
			});
		}
	}
	isEmpty.value = applications.value.length === 0;
	loading.value = false;
};

watch(
	() => appStore.pageData,
	async (newValue) => {
		if (newValue.length > 0) {
			if (route.params.categories || route.params.type) {
				await fetchData();
			}
		}
	},
	{
		immediate: true
	}
);
</script>
<style lang="scss" scoped>
.list-page {
	width: 100%;
	height: calc(100% - 56px);
	padding: 0 44px 56px;
}

.empty_view {
	width: 100%;
	height: calc(100% - 56px);
}
</style>
