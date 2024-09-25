<template>
	<page-container>
		<template v-slot:page>
			<div :class="appStore.isPublic ? 'home-page-public' : 'home-page'">
				<app-store-body
					v-if="appStore.isPublic"
					:title="t('main.discover')"
					:bottom-separator="true"
					:padding-exclude-body="44"
				/>
				<bt-banner
					v-else
					:size="3"
					class="home-padding"
					style="padding-top: 20px; padding-bottom: 20px"
				>
					<template v-slot:slide="{ index }">
						<q-img
							style="border-radius: 12px"
							width="100%"
							ratio="4.42"
							:src="getRequireImage(`banner/home_banner${index}.png`)"
						/>
					</template>
				</bt-banner>

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
										@click="onTopicClick(item, index)"
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
						class="home-padding"
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
						class="home-padding"
						:loading="topLoading"
						:show-body="topApps.length > 0"
						label="Top App on Terminus"
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
						class="home-padding"
						:loading="latestLoading"
						:show-body="latestApps.length > 0"
						label="Latest App on Terminus"
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
import { ref, onMounted, watch } from 'vue';
import { useRouter } from 'vue-router';
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
import BtBanner from 'components/base/BtBanner.vue';
import { getRequireImage } from 'src/utils/imageUtils';
import TopicView from 'components/topic/TopicView.vue';
import TopicAppView from 'components/topic/TopicAppView.vue';
import AppCard from 'components/appcard/AppCard.vue';
import { useI18n } from 'vue-i18n';

const router = useRouter();
const topApps = ref<AppStoreInfo[]>([]);
const latestApps = ref<AppStoreInfo[]>([]);
const pageLoading = ref(true);
const topLoading = ref(true);
const latestLoading = ref(true);
const pageData = ref();
const appStore = useAppStore();
const { t } = useI18n();

function fetchData(showLoading = false) {
	if (showLoading) {
		pageLoading.value = true;
		topLoading.value = true;
		latestLoading.value = true;
	}
	appStore
		.getPageData(CATEGORIES_TYPE.LOCAL.ALL)
		.then((all) => {
			if (all && all.data) {
				pageData.value = all.data;

				const top = pageData.value.find(
					(item: any) => item.type === 'Default Topic' && item.id === 'Hottest'
				);
				if (top) {
					getTop()
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
					getLatest()
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

watch(
	() => appStore.pageData,
	(newValue) => {
		if (newValue.length > 0) {
			fetchData(true);
		}
	},
	{
		immediate: true
	}
);

const clickList = (type: string) => {
	router.push({
		name: TRANSACTION_PAGE.List,
		params: {
			categories: CATEGORIES_TYPE.LOCAL.ALL,
			type: type
		}
	});
};

const onTopicClick = (item: any, index: number) => {
	if (item) {
		// router.push({
		// 	name: TRANSACTION_PAGE.Discover,
		// 	params: {
		// 		categories: CATEGORIES_TYPE.LOCAL.ALL,
		// 		topicId: index
		// 	}
		// });
	}
};
</script>
<style lang="scss" scoped>
.home-page {
	height: 100%;
	width: 100%;
	padding: 0 0 56px 0;

	.home-padding {
		padding-left: 44px;
		padding-right: 44px;
	}

	.empty_view {
		height: 120px;
		width: 100%;
	}
}

.home-page-public {
	height: calc(100% - 56px);
	margin-top: 56px;
	padding: 0 0 56px;

	.home-padding {
		padding-left: 44px;
		padding-right: 44px;
	}

	.empty_view {
		height: 120px;
		width: 100%;
	}
}
</style>
