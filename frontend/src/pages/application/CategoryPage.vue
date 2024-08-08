<template>
	<page-container>
		<template v-slot:page>
			<div class="category-page">
				<app-store-body
					:title="categoryRef"
					:bottom-separator="true"
					:padding-exclude-body="44"
				/>
				<template v-for="(item, index) in pageData" :key="index">
					<app-store-body
						v-if="item.type === 'Topic' && item.topicType === 'Discover'"
						:label="item.name"
						:padding-exclude-body="44"
						:bottom-separator="true"
						:loading="pageLoading"
						:show-body="item.content.length > 0"
						:body-margin-bottom="32"
					>
						<template v-slot:loading>
							<app-card-grid rule="app-store-topic" show-size="5,3,2">
								<template v-slot:card>
									<topic-view :skeleton="true" />
								</template>
							</app-card-grid>
						</template>
						<template v-slot:body>
							<app-store-swiper
								:data-array="item.content"
								:navigation-offsite="40"
							>
								<template v-slot:swiper="{ item, index }">
									<topic-view
										:item="{ ...item, id: index }"
										@click="onItemClick(item, index)"
									/>
								</template>
							</app-store-swiper>
						</template>
					</app-store-body>

					<app-store-body
						v-if="item.type === 'Topic' && item.topicType === 'Categories'"
						:label="item.name"
						:padding-exclude-body="44"
						:bottom-separator="true"
						:loading="pageLoading"
						:show-body="item.content.length > 0"
						:body-margin-bottom="32"
					>
						<template v-slot:loading>
							<app-card-grid rule="app-store-topic" show-size="5,3,2">
								<template v-slot:card>
									<topic-app-view :skeleton="true" />
								</template>
							</app-card-grid>
						</template>
						<template v-slot:body>
							<app-store-swiper
								:data-array="item.content"
								:navigation-offsite="40"
							>
								<template v-slot:swiper="{ item }">
									<topic-app-view :item="item" />
								</template>
							</app-store-swiper>
						</template>
					</app-store-body>

					<app-store-body
						v-if="item.type === 'Recommends'"
						class="category-padding"
						:loading="pageLoading"
						:show-body="item.content.length > 0"
						:label="item.name"
						:right="t('base.see_all')"
						:no-label-padding-bottom="true"
						:bottom-separator="true"
						@on-right-click="clickList(CATEGORIES_TYPE.LOCAL.RECOMMENDS)"
					>
						<template v-slot:loading>
							<app-card-grid rule="app-store-application">
								<template v-slot:card>
									<app-card :skeleton="true" />
								</template>
							</app-card-grid>
						</template>
						<template v-slot:body>
							<app-card-grid
								rule="app-store-application"
								:app-list="item.content"
							>
								<template v-slot:card="{ item }">
									<app-card :item="item" />
								</template>
							</app-card-grid>
						</template>
					</app-store-body>

					<app-store-body
						v-if="item.type === 'Default Topic' && item.id === 'Hottest'"
						class="category-padding"
						:loading="topLoading"
						:show-body="topApps.length > 0"
						:label="topTitle"
						:right="t('base.see_all')"
						:no-label-padding-bottom="true"
						:bottom-separator="true"
						@on-right-click="clickList(CATEGORIES_TYPE.LOCAL.TOP)"
					>
						<template v-slot:loading>
							<app-card-grid rule="app-store-application">
								<template v-slot:card>
									<app-card :skeleton="true" />
								</template>
							</app-card-grid>
						</template>
						<template v-slot:body>
							<app-card-grid rule="app-store-application" :app-list="topApps">
								<template v-slot:card="{ item }">
									<app-card :item="item" />
								</template>
							</app-card-grid>
						</template>
					</app-store-body>

					<app-store-body
						v-if="item.type === 'Default Topic' && item.id === 'Newest'"
						class="category-padding"
						:loading="latestLoading"
						:show-body="latestApps.length > 0"
						:label="latestTitle"
						:right="t('base.see_all')"
						:no-label-padding-bottom="true"
						@on-right-click="clickList(CATEGORIES_TYPE.LOCAL.LATEST)"
					>
						<template v-slot:loading>
							<app-card-grid rule="app-store-application">
								<template v-slot:card>
									<app-card :skeleton="true" />
								</template>
							</app-card-grid>
						</template>
						<template v-slot:body>
							<app-card-grid
								rule="app-store-application"
								:app-list="latestApps"
							>
								<template v-slot:card="{ item }">
									<app-card :item="item" />
								</template>
							</app-card-grid>
						</template>
					</app-store-body>
				</template>
			</div>
		</template>
	</page-container>
</template>

<script lang="ts" setup>
import { ref, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
	AppStoreInfo,
	CATEGORIES_TYPE,
	TRANSACTION_PAGE
} from 'src/constants/constants';
import PageContainer from 'components/base/PageContainer.vue';
import AppStoreBody from 'components/base/AppStoreBody.vue';
import { getTop, getLatest } from 'src/api/storeApi';
import AppStoreSwiper from 'components/base/AppStoreSwiper.vue';
import AppCardGrid from 'components/appcard/AppCardGrid.vue';
import { useAppStore } from 'src/stores/app';
import TopicView from 'components/topic/TopicView.vue';
import TopicAppView from 'components/topic/TopicAppView.vue';
import AppCard from 'components/appcard/AppCard.vue';
import { useI18n } from 'vue-i18n';

const router = useRouter();
const route = useRoute();

const topApps = ref<AppStoreInfo[]>([]);
const latestApps = ref<AppStoreInfo[]>([]);
const categoryRef = ref(route.params.categories as string);
const topTitle = ref();
const latestTitle = ref();
const pageData = ref();
const appStore = useAppStore();
const pageLoading = ref(true);
const topLoading = ref(true);
const latestLoading = ref(true);
const { t } = useI18n();

onMounted(() => {
	topTitle.value = `Top App in ${categoryRef.value}`;
	latestTitle.value = `Latest App in ${categoryRef.value}`;
	fetchData(true);
});

function fetchData(showLoading = false) {
	if (showLoading) {
		pageLoading.value = true;
		topLoading.value = true;
		latestLoading.value = true;
	}
	appStore
		.getPageData(categoryRef.value)
		.then((all) => {
			if (all && all.data) {
				pageData.value = all.data;
				const top = pageData.value.find(
					(item: any) => item.type === 'Default Topic' && item.id === 'Hottest'
				);
				if (top) {
					getTop(categoryRef.value.toLowerCase())
						.then((list) => {
							topApps.value = list;
						})
						.finally(() => {
							topLoading.value = false;
						});
				}

				const latest = pageData.value.find(
					(item: any) => item.type === 'Default Topic' && item.id === 'Newest'
				);
				if (latest) {
					getLatest(categoryRef.value.toLowerCase())
						.then((list) => {
							latestApps.value = list;
						})
						.finally(() => {
							latestLoading.value = false;
						});
				}
			}
		})
		.finally(() => {
			pageLoading.value = false;
		});
}

const clickList = (type: string) => {
	router.push({
		name: TRANSACTION_PAGE.List,
		params: {
			categories: categoryRef.value,
			type: type
		}
	});
};

const onItemClick = (item: any, index: number) => {
	if (item) {
		router.push({
			name: TRANSACTION_PAGE.Discover,
			params: {
				categories: categoryRef.value,
				topicId: index
			}
		});
	}
};
</script>
<style lang="scss" scoped>
.category-page {
	height: calc(100% - 56px);
	margin-top: 56px;
	width: 100%;
	padding: 0 0 56px;

	.category-padding {
		padding-left: 44px;
		padding-right: 44px;
	}

	.empty_view {
		height: 120px;
		width: 100%;
	}
}
</style>
