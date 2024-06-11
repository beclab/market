<template>
	<page-container v-model="showShadow" :title-height="56">
		<template v-slot:title>
			<title-bar
				:show="true"
				:title="titleRef"
				@onReturn="Router.back()"
				:shadow="showShadow"
			/>
		</template>
		<template v-slot:page>
			<div class="discover-page column justify-start items-start">
				<q-img class="discover-details-img" ratio="2" :src="showImgRef">
					<template v-slot:loading>
						<q-skeleton class="discover-details-img" style="height: 100%" />
					</template>
				</q-img>
				<div
					class="column justify-center items-center"
					:class="
						$q.dark.isActive
							? 'discover-details_app_bg-dark'
							: 'discover-details_app_bg'
					"
					v-show="appListRef.length > 0"
				>
					<app-small-card
						v-for="(item, index) in appListRef"
						:key="item.name"
						:item="item"
						:is-last-line="index === appListRef.length - 1"
						v-show="appListRef"
					/>
				</div>
				<div id="discover-html" class="discover-rich" v-html="richRef" />
			</div>
		</template>
	</page-container>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { AppStoreInfo, TopicInfo } from 'src/constants/constants';
import { useAppStore } from 'src/stores/app';
import TitleBar from 'components/base/TitleBar.vue';
import { fromBase64 } from 'js-base64';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';
import PageContainer from 'components/base/PageContainer.vue';
import AppSmallCard from 'components/appcard/AppSmallCard.vue';
import { useQuasar } from 'quasar';

const Router = useRouter();
const richRef = ref();
const showImgRef = ref();
const labelRef = ref();
const titleRef = ref();
const discoverRef = ref<TopicInfo>();
const appListRef = ref<AppStoreInfo[]>([]);
const route = useRoute();
const appStore = useAppStore();
const showShadow = ref(false);
const $q = useQuasar();

onMounted(async () => {
	const topicId = Number(route.params.topicId);
	const category = route.params.categories as string;
	let discoverArray: TopicInfo[];
	const categoryData = await appStore.getPageData(category);
	if (categoryData) {
		const topic = categoryData.data.find((item: any) => {
			return item.type === 'Topic';
		});
		if (topic) {
			discoverArray = topic.content;
			const data = discoverArray.find((item, index) => {
				return topicId === index;
			});
			setValue(data);
			bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
		}
	}
});

const setValue = (discoverApp: TopicInfo | null | undefined) => {
	if (discoverApp) {
		console.log(discoverApp);
		discoverRef.value = discoverApp;
		appListRef.value = discoverApp.apps ? discoverApp.apps : [];
		showImgRef.value = discoverApp.detailimg;
		richRef.value = getRichText(discoverApp.richtext);
		labelRef.value = discoverApp.name;
		titleRef.value = discoverApp.introduction;
	}
};

const updateApp = (app: AppStoreInfo) => {
	updateAppStoreList(appListRef.value, app);
};

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

const getRichText = (richText: string) => {
	try {
		return fromBase64(richText);
	} catch (e) {
		return '';
	}
};
</script>

<style scoped lang="scss">
.discover-page {
	width: 100%;
	height: 100%;
	padding-left: 80px;
	padding-right: 80px;

	.discover-details-img {
		width: 100%;
		border-radius: 12px;
	}

	.discover-details_app_bg {
		height: auto;
		padding: 16px;
		margin-top: 20px;
		margin-bottom: 20px;
		background: linear-gradient(90deg, #ebf1ff 0%, #ffffff 100%);
		backdrop-filter: blur(6.07811px);
		border-radius: 8px;
		border: 1px solid $separator;
		width: 100%;
	}

	.discover-details_app_bg-dark {
		@extend .discover-details_app_bg;
		background: linear-gradient(90deg, #222637 0%, #1f1f1f 100%);
	}

	.discover-rich {
		width: calc(100% - 10px);
		margin-top: 20px;
	}
}
</style>
