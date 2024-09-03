<template>
	<page-container>
		<template v-slot:page>
			<div class="recommend-page">
				<!--				<app-store-body-->
				<!--					class="recommend-padding"-->
				<!--					:show-body="true"-->
				<!--					:title="categoryRef"-->
				<!--				>-->
				<!--					<template v-slot:body>-->
				<!--						<bt-banner :size="1">-->
				<!--							<template v-slot:slide="{ index }">-->
				<!--								<q-img-->
				<!--									style="border-radius: 12px; position: relative"-->
				<!--									width="100%"-->
				<!--									ratio="4.42"-->
				<!--									:src="-->
				<!--										getRequireImage(-->
				<!--											`banner/${categoryRef.toLowerCase()}_banner${index}.png`-->
				<!--										)-->
				<!--									"-->
				<!--								>-->
				<!--									&lt;!&ndash;                  <div&ndash;&gt;-->
				<!--									&lt;!&ndash;                    class="app-card-background"&ndash;&gt;-->
				<!--									&lt;!&ndash;                    v-if="latestApps[0]">&ndash;&gt;-->
				<!--									&lt;!&ndash;                    <app-card&ndash;&gt;-->
				<!--									&lt;!&ndash;                      :item="latestApps[0]"&ndash;&gt;-->
				<!--									&lt;!&ndash;                      :type="CFG_TYPE.WORK_FLOW"&ndash;&gt;-->
				<!--									&lt;!&ndash;                      :is-last-line="true"&ndash;&gt;-->
				<!--									&lt;!&ndash;                      style="height: 100%"&ndash;&gt;-->
				<!--									&lt;!&ndash;                    />&ndash;&gt;-->
				<!--									&lt;!&ndash;                  </div>&ndash;&gt;-->
				<!--								</q-img>-->
				<!--							</template>-->
				<!--						</bt-banner>-->
				<!--					</template>-->
				<!--				</app-store-body>-->

				<template v-for="(item, index) in pageData" :key="index">
					<app-store-body
						v-if="item.type === 'Default Topic' && item.id === 'Newest'"
						class="recommend-padding"
						:title="t('main.recommendation')"
						:loading="latestLoading"
						:show-body="latestApps.length > 0"
					>
						<template v-slot:loading>
							<app-card-grid rule="app-store-workflow">
								<template v-slot:card>
									<my-app-card :skeleton="true" />
								</template>
							</app-card-grid>
						</template>
						<template v-slot:body>
							<app-card-grid rule="app-store-workflow" :app-list="latestApps">
								<template v-slot:card="{ item }">
									<my-app-card :item="item" :tag="CFG_TYPE.WORK_FLOW" />
								</template>
							</app-card-grid>
						</template>
					</app-store-body>
				</template>
			</div>
		</template>
	</page-container>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import AppStoreBody from 'src/components/base/AppStoreBody.vue';
import MyAppCard from 'src/components/appcard/MyAppCard.vue';
import { AppStoreInfo, CATEGORIES_TYPE } from 'src/constants/constants';
import { getRecommendApps } from 'src/api/storeApi';
import PageContainer from 'src/components/base/PageContainer.vue';
// import BtBanner from 'components/base/BtBanner.vue';
// import { getRequireImage } from 'src/utils/imageUtils';
import { useAppStore } from 'src/stores/app';
import AppCardGrid from 'components/appcard/AppCardGrid.vue';
import { useI18n } from 'vue-i18n';
import { CFG_TYPE } from 'src/constants/config';

const latestApps = ref<AppStoreInfo[]>([]);
const { t } = useI18n();

const pageLoading = ref(true);
const latestLoading = ref(true);
const pageData = ref();
const appStore = useAppStore();

onMounted(() => {
	fetchData(true);
});

async function fetchData(showLoading = false) {
	if (showLoading) {
		pageLoading.value = true;
		latestLoading.value = true;
	}
	appStore
		.getPageData(CATEGORIES_TYPE.LOCAL.ALL)
		.then((all) => {
			console.log(all);
			if (all && all.data) {
				pageData.value = all.data;
				pageLoading.value = false;

				const latest = pageData.value.find(
					(item: any) => item.type === 'Default Topic' && item.id === 'Newest'
				);
				if (latest) {
					getRecommendApps()
						.then((data) => {
							latestApps.value = data;
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
</script>

<style lang="scss">
.recommend-page {
	height: 100%;
	width: 100%;
	padding: 56px 0;

	.recommend-padding {
		padding-left: 44px;
		padding-right: 44px;
	}

	.app-card-background {
		position: absolute;
		background: white;
		border-radius: 12px;
		bottom: 20%;
		left: 44px;
		width: 45%;
		height: 40%;
		min-height: 104px;
	}

	.empty_view {
		height: 120px;
		width: 100%;
	}

	.full-view {
		width: 100%;
		height: 100%;
	}
}
</style>
