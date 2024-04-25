<template>
	<page-container :is-app-details="true" :title-height="56">
		<template v-slot:title>
			<app-title-bar :item="item" :show-header-bar="true" />
		</template>
		<template v-slot:page>
			<div class="app-image-preview-page column justify-center items-center">
				<app-store-swiper
					style="margin-bottom: 20px"
					ref="swiper"
					:ratio="1.6"
					:max-height="imageHeight"
					v-if="item && item.promoteImage"
					:data-array="item && item.promoteImage ? item.promoteImage : []"
					:slides-per-view="1"
				>
					<template v-slot:swiper="{ item }">
						<q-img class="promote-img" ratio="1.6" :src="item">
							<template v-slot:loading>
								<q-skeleton class="promote-img" style="height: 100%" />
							</template>
						</q-img>
					</template>
				</app-store-swiper>
			</div>
		</template>
	</page-container>
</template>

<script lang="ts" setup>
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { AppStoreInfo } from 'src/constants/constants';
import PageContainer from 'components/base/PageContainer.vue';
import AppStoreSwiper from 'components/base/AppStoreSwiper.vue';
import AppTitleBar from 'components/appintro/AppTitleBar.vue';
import { getApp } from 'src/api/storeApi';
import { useRoute } from 'vue-router';
import { useAppStore } from 'src/stores/app';
import { useQuasar } from 'quasar';

const item = ref<AppStoreInfo>();
const route = useRoute();
const appStore = useAppStore();
const initialSlide = ref(0);
const swiper = ref();
const imageHeight = ref(0);
const $q = useQuasar();
let resizeTimer: NodeJS.Timeout | null = null;

onMounted(async () => {
	await setAppItem(route.params.name as string);
	swiper.value.slideTo(Number(route.params.index));
	console.log(initialSlide.value);

	updateImageHeight();
	window.addEventListener('resize', resize);
});

onBeforeUnmount(() => {
	window.removeEventListener('resize', resize);
});

const resize = () => {
	if (resizeTimer) {
		clearTimeout(resizeTimer);
	}
	resizeTimer = setTimeout(function () {
		updateImageHeight();
	}, 200);
};

const updateImageHeight = () => {
	imageHeight.value = $q.screen.height - 56 - 40;
};

const setAppItem = async (name: string) => {
	if (!name) {
		console.log('app name empty');
		return;
	}

	let app = appStore.getAppItem(name);
	if (app) {
		item.value = app;
		console.log('load local app');
	} else {
		const response = await getApp(name);
		if (!response) {
			console.log('get app info failure');
			return;
		}
		appStore.tempAppMap[name] = response;
		item.value = response;
	}

	console.log(item.value);
};
</script>
<style lang="scss" scoped>
.app-image-preview-page {
	width: 100%;
	height: 100%;
	padding-top: 20px;

	.promote-img {
		border-radius: 20px;
		width: 100%;
	}
}
</style>
