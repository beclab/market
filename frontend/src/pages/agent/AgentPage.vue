<template>
	<page-container>
		<template v-slot:page>
			<div class="recommend-page">
				<!--				<app-store-body-->
				<!--					class="recommend-padding"-->
				<!--					:show-body="true"-->
				<!--					:title="pageTitle"-->
				<!--					:body-margin-bottom="32"-->
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
				<!--								</q-img>-->
				<!--							</template>-->
				<!--						</bt-banner>-->
				<!--					</template>-->
				<!--				</app-store-body>-->

				<app-store-body
					class="recommend-padding"
					:title="t('agent.large_language_model')"
					:loading="pageLoading"
					:body-margin-bottom="32"
					:show-body="modelList.length > 0"
				>
					<template v-slot:loading>
						<app-card-grid rule="app-store-workflow">
							<template v-slot:card>
								<my-app-card :skeleton="true" />
							</template>
						</app-card-grid>
					</template>
					<template v-slot:body>
						<div class="app-store-workflow">
							<template v-for="item in modelList" :key="item.id">
								<my-app-card :item="item" :tag="CFG_TYPE.MODEL" />
							</template>
						</div>
					</template>
				</app-store-body>
			</div>
		</template>
	</page-container>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import AppStoreBody from 'components/base/AppStoreBody.vue';
import PageContainer from 'components/base/PageContainer.vue';
import { AppStoreInfo, CFG_TYPE } from 'src/constants/constants';
import { useI18n } from 'vue-i18n';
import MyAppCard from 'src/components/appcard/MyAppCard.vue';
import { getModels } from 'src/api/difyApi';
import AppCardGrid from 'src/components/appcard/AppCardGrid.vue';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';

const modelList = ref<AppStoreInfo[]>([]);
const pageLoading = ref(true);
const { t } = useI18n();

const updateApp = (app: AppStoreInfo) => {
	console.log('Model update', app.name + app.progress + app.status);
	updateAppStoreList(modelList.value, app);
};

onMounted(async () => {
	bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
	await fetchData();
});

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

async function fetchData(showLoading = false) {
	if (showLoading) {
		pageLoading.value = true;
	}

	modelList.value = await getModels();

	console.log(modelList.value);

	pageLoading.value = false;
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
