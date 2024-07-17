<template>
	<page-container :title-height="56">
		<template v-slot:title>
			<title-bar :show="true" @onReturn="router.back()" />
		</template>
		<template v-slot:page>
			<div class="update-scroll">
				<app-store-body
					:title="t('my.available_updates')"
					:show-body="true"
					:title-separator="true"
					@on-right-click="onUpdateAll"
					:right="appStore.updateApps.length === 0 ? '' : t('my.update_all')"
				>
					<template v-slot:body>
						<empty-view
							v-if="appStore.updateApps.length === 0"
							:label="t('my.everything_up_to_date')"
							class="empty_view"
						/>
						<div class="app-store-workflow" style="margin-top: 32px">
							<template v-for="item in appStore.updateApps" :key="item.name">
								<my-app-card :item="item" :is-update="true" :version="true" />
							</template>
							<app-card-hide-border />
						</div>
					</template>
				</app-store-body>
			</div>
		</template>
	</page-container>
</template>

<script setup lang="ts">
import EmptyView from 'components/base/EmptyView.vue';
import { onMounted } from 'vue';
import { useAppStore } from 'src/stores/app';
import PageContainer from 'components/base/PageContainer.vue';
import AppStoreBody from 'src/components/base/AppStoreBody.vue';
import AppCardHideBorder from 'components/appcard/AppCardHideBorder.vue';
import TitleBar from 'components/base/TitleBar.vue';
import { useRouter } from 'vue-router';
import MyAppCard from 'src/components/appcard/MyAppCard.vue';
import { useI18n } from 'vue-i18n';

const router = useRouter();
const appStore = useAppStore();
const { t } = useI18n();

onMounted(async () => {
	await appStore.loadApps();
});

const onUpdateAll = () => {
	appStore.updateApps.forEach((app) => {
		appStore.upgradeApp(app);
	});
};
</script>

<style lang="scss">
.update-scroll {
	width: 100%;
	height: calc(100vh - 56px);
	padding: 0 44px;

	.empty_view {
		width: 100%;
		height: calc(100vh - 112px - 64px);
	}
}
</style>
