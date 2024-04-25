<template>
	<page-container>
		<template v-slot:page>
			<empty-view v-if="isEmpty" title="Search" class="empty_view" />
			<div class="search-page" v-else>
				<div class="app-store-application" v-if="loading">
					<template v-for="item in 6" :key="item">
						<app-card :skeleton="true" />
					</template>
					<app-card-hide-border />
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

<script setup lang="ts">
import EmptyView from 'components/base/EmptyView.vue';
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { searchApplications } from 'src/api/storeApi';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';
import { AppStoreInfo } from 'src/constants/constants';
import PageContainer from 'components/base/PageContainer.vue';
import AppCard from 'components/appcard/AppCard.vue';
import AppCardHideBorder from 'components/appcard/AppCardHideBorder.vue';

const Route = useRoute();
const applications = ref<AppStoreInfo[]>([]);
const loading = ref(false);
const isEmpty = ref(false);

async function fetchData(newValue?: any) {
	loading.value = true;
	if (newValue) {
		applications.value = await searchApplications(newValue);
	} else {
		applications.value = [];
	}
	console.log(applications.value);
	isEmpty.value = applications.value.length == 0;
	console.log(isEmpty.value);
	loading.value = false;
}

const updateApp = (app: AppStoreInfo) => {
	console.log(`search update status ${app.name}`);
	updateAppStoreList(applications.value, app);
};

onMounted(async () => {
	await fetchData(Route.query.q);
	console.log(1);
	bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});
</script>

<style lang="scss" scoped>
.empty_view {
	width: 100%;
	height: 100vh;
}

.search-page {
	width: 100%;
	height: 100%;
	padding-left: 32px;
	padding-right: 32px;
	padding-top: 20px;
}
</style>
