<template>
	<app-store-body
		v-if="
			appStore.installApps.filter((app) => app.source === SOURCE_TYPE.Market)
				.length > 0
		"
		:show-body="true"
		:no-label-padding-bottom="true"
	>
		<template v-slot:body>
			<div class="app-store-workflow">
				<template
					v-for="item in appStore.installApps.filter(
						(app) => app.source === SOURCE_TYPE.Market
					)"
					:key="item.name"
				>
					<my-app-card :item="item" :version="true" :manager="true" />
				</template>
				<app-card-hide-border />
			</div>
		</template>
	</app-store-body>
	<empty-view
		v-else
		:label="i18n.t('no_installed_app_tips')"
		class="full-view"
	/>
</template>

<script setup lang="ts">
import EmptyView from 'components/base/EmptyView.vue';
import { useAppStore } from 'src/stores/app';
import AppStoreBody from 'components/base/AppStoreBody.vue';
import AppCardHideBorder from 'components/appcard/AppCardHideBorder.vue';
import MyAppCard from 'components/appcard/MyAppCard.vue';
import { SOURCE_TYPE } from 'src/constants/constants';
import { useI18n } from 'vue-i18n';

const i18n = useI18n();

const appStore = useAppStore();
</script>

<style lang="scss">
::-webkit-scrollbar {
	/*隐藏滚轮*/
	display: none;
}

.full-view {
	width: 100%;
	height: 100%;
}
</style>
