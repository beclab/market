<template>
	<q-dialog class="card-dialog-pc" ref="dialogRef">
		<q-card class="card-container-pc column no-shadow">
			<base-dialog-bar
				:label="t('detail.reference_app_not_installed')"
				@close="onDialogCancel"
			/>
			<div class="q-pa-lg dialog-scroll">
				<div class="text-ink-2 text-body2">
					{{ t('detail.need_reference_app_to_use') }}
				</div>
				<template :key="app.name" v-for="(app, index) in references">
					<app-small-card
						:item="app"
						:is-last-line="index === references.length - 1"
					/>
				</template>

				<div class="full-width row justify-end q-mt-lg">
					<q-btn
						class="bg-blue-default btn-ok text-subtitle1 text-white"
						:label="t('base.ok')"
						@click="onDialogOK"
					/>
				</div>
			</div>
		</q-card>
	</q-dialog>
</template>

<script lang="ts" setup>
import { useDialogPluginComponent } from 'quasar';
import AppSmallCard from 'src/components/appcard/AppSmallCard.vue';
import { AppStoreInfo } from 'src/constants/constants';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';
import { onBeforeUnmount, onMounted, PropType, ref } from 'vue';
import { getApp } from 'src/api/storeApi';
import { useI18n } from 'vue-i18n';
import BaseDialogBar from 'src/components/base/BaseDialogBar.vue';

const props = defineProps({
	app: {
		type: Object as PropType<AppStoreInfo>,
		required: true
	}
});

const references = ref<AppStoreInfo[]>([]);
const { onDialogOK, onDialogCancel, dialogRef } = useDialogPluginComponent();
const { t } = useI18n();

const updateApp = (app: AppStoreInfo) => {
	console.log(`get status ${app.name}`);
	if (app) {
		updateAppStoreList(references.value, app);
	}
};

onMounted(() => {
	bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);

	if (
		props.app?.options &&
		props.app?.options.appScope &&
		props.app?.options.appScope.appRef &&
		props.app?.options.appScope.appRef.length > 0
	) {
		props.app?.options.appScope.appRef.forEach((appName) => {
			getApp(appName).then((app) => {
				if (app) {
					references.value.push(app);
				}
			});
		});
	}
});

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});
</script>

<style scoped lang="scss">
.card-dialog-pc {
	.card-container-pc {
		border-radius: 12px;
		width: 400px;

		.dialog-scroll {
			width: 400px;
			max-height: 396px !important;

			.btn-ok {
				width: 100px;
				height: 40px;
			}
		}
	}
}
</style>
