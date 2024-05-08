<template>
	<div
		class="row install_btn_bg"
		@click.stop
		:style="{
			'--textColor': textColor,
			'--backgroundColor': backgroundColor,
			'--border': border,
			'--width': larger ? '88px' : '72px',
			'--statusWidth': larger
				? showDropMenu
					? 'calc(100% - 21px)'
					: '100%'
				: showDropMenu
					? 'calc(100% - 17px)'
					: '100%'
		}"
	>
		<q-btn
			:loading="isLoading"
			:class="larger ? 'application_install_larger' : 'application_install'"
			:style="{
				'--radius': showDropMenu ? '0px' : larger ? '8px' : '4px'
			}"
			@click="onClick"
			@mouseover="updateHover(true)"
			@mouseleave="updateHover(false)"
			dense
			:percentage="item.progress ? item.progress : undefined"
			flat
			no-caps
		>
			<div>{{ status }}</div>

			<template v-slot:loading>
				<div
					style="width: 100%; height: 100%"
					class="row justify-center items-center"
					v-if="
						item.status === APP_STATUS.uninstalling ||
						item.status === APP_STATUS.upgrading ||
						(item.status === APP_STATUS.installing &&
							item.cfgType !== CFG_TYPE.MODEL)
					"
				>
					{{ status }}
				</div>
				<div
					style="width: 100%; height: 100%"
					class="row justify-center items-center"
					v-if="
						item.status === APP_STATUS.installing &&
						item.cfgType === CFG_TYPE.MODEL
					"
				>
					<!--          <q-spinner-hourglass size="12px" style="margin-right: 4px"/>-->
					{{ Number(item.progress) + '%' }}
				</div>
				<div
					v-if="
						item.status === APP_STATUS.pending ||
						item.status === APP_STATUS.waiting ||
						item.status === APP_STATUS.resuming
					"
				>
					<q-img
						class="pending-image"
						:src="getRequireImage('pending_loading.png')"
					/>
				</div>
			</template>
		</q-btn>
		<div
			v-if="showDropMenu"
			class="install_btn_separator_bg items-center"
			:style="{ background: backgroundColor, height: larger ? '32px' : '24px' }"
		>
			<div class="install_btn_separator" />
		</div>
		<q-btn-dropdown
			v-if="showDropMenu"
			dropdown-icon="img:/arrow.svg"
			size="10px"
			:class="
				larger ? 'application_install_larger_more' : 'application_install_more'
			"
			flat
			dense
		>
			<q-list>
				<q-item clickable v-close-popup @click="onUninstall">
					<q-item-section>
						<q-item-label>{{ t('app.uninstall') }}</q-item-label>
					</q-item-section>
				</q-item>
				<q-item
					clickable
					v-close-popup
					v-if="
						item.cfgType === CFG_TYPE.MODEL &&
						item.status === APP_STATUS.installed
					"
					@click="onLoad"
				>
					<q-item-section>
						<q-item-label>{{ t('app.load') }}</q-item-label>
					</q-item-section>
				</q-item>
				<q-item
					clickable
					v-close-popup
					v-if="
						item.cfgType === CFG_TYPE.MODEL &&
						item.status === APP_STATUS.running
					"
					@click="onUnload"
				>
					<q-item-section>
						<q-item-label>{{ t('app.unload') }}</q-item-label>
					</q-item-section>
				</q-item>
				<q-item clickable v-close-popup v-if="isUpdate" @click="onUpdateOpen">
					<q-item-section>
						<q-item-label>{{ t('app.open') }}</q-item-label>
					</q-item-section>
				</q-item>
			</q-list>
		</q-btn-dropdown>
	</div>
</template>

<script lang="ts" setup>
import { computed, PropType, ref, watch } from 'vue';
import { APP_STATUS, AppStoreInfo, CFG_TYPE } from 'src/constants/constants';
import { useAppStore } from 'src/stores/app';
import { openApplication } from 'src/api/private/operations';
import { getRequireImage } from 'src/utils/imageUtils';
import { useI18n } from 'vue-i18n';
import { useUserStore } from 'src/stores/user';

const props = defineProps({
	item: {
		type: Object as PropType<AppStoreInfo>,
		required: true
	},
	development: {
		type: Boolean,
		required: false
	},
	larger: {
		type: Boolean,
		required: false,
		default: false
	},
	isUpdate: {
		type: Boolean,
		required: false,
		default: false
	},
	manager: {
		type: Boolean,
		require: false,
		default: false
	}
});

const appStore = useAppStore();
const userStore = useUserStore();
const status = ref();
const isDisabled = ref(false);
const isLoading = ref<boolean>(false);
const hoverRef = ref(false);
const textColor = ref();
const backgroundColor = ref();
const border = ref();
let hasCheck = false;
const { t } = useI18n();

const showDropMenu = computed(() => {
	return (
		props.manager &&
		(props.item.status === APP_STATUS.running ||
			props.item.status === APP_STATUS.suspend ||
			props.item.status === APP_STATUS.installed)
	);
});

async function onClick() {
	if (!props.item) {
		return;
	}

	switch (props.item?.status) {
		case APP_STATUS.preflightFailed:
			//DO NOTHING
			break;
		case APP_STATUS.uninstalled:
			userStore.frontendPreflight(props.item);
			break;
		case APP_STATUS.installable:
			appStore.installApp(props.item, props.development);
			break;
		case APP_STATUS.pending:
		case APP_STATUS.installing:
			console.log(props.item?.name);
			console.log('cancel installing');
			if (props.item?.cfgType !== CFG_TYPE.WORK_FLOW) {
				appStore.cancelInstallingApp(props.item, props.development);
			}
			break;
		case APP_STATUS.suspend:
			appStore.resumeApp(props.item, props.development);
			break;
		case APP_STATUS.running: {
			console.log(props.item);
			if (props.isUpdate) {
				appStore.upgradeApp(props.item);
			} else {
				openApp();
			}
			break;
		}
	}
}

const openApp = () => {
	if (!props.item) {
		return;
	}

	if (props.item?.cfgType === CFG_TYPE.APPLICATION) {
		let app = userStore.myApps.find((app: any) => app.id == props.item?.id);
		if (!app) {
			return;
		}
		console.log(app);

		const entrance = app.entrances?.find((entrance) => !entrance.invisible);
		console.log(entrance);

		if (!entrance) {
			return;
		}

		if (window.top == window) {
			const href = window.location.href;
			console.log(href);
			const host = href.split('//')[1];
			console.log(host);
			const isLocal = host.startsWith('market.local');
			if (isLocal) {
				const s = entrance.url.split('.');
				s.splice(1, 0, 'local');
				const url = s.join('.');
				console.log(url);
				window.open('//' + url, '_blank');
			} else {
				window.open('//' + entrance.url, '_blank');
			}
		} else {
			openApplication(entrance.id);
		}
	} else {
		return;
	}
};

async function onLoad() {
	if (!props.item) {
		return;
	}

	switch (props.item?.status) {
		case APP_STATUS.installed:
			appStore.resumeApp(props.item, props.development);
			break;
	}
}

async function onUnload() {
	if (!props.item) {
		return;
	}

	switch (props.item?.status) {
		case APP_STATUS.running:
			appStore.suspendApp(props.item, props.development);
			break;
	}
}

async function onUninstall() {
	if (!props.item) {
		return;
	}
	switch (props.item?.status) {
		case APP_STATUS.running:
		case APP_STATUS.suspend:
		case APP_STATUS.installed:
			appStore.uninstallApp(props.item, props.development);
			break;
	}
}

async function onUpdateOpen() {
	if (!props.item) {
		return;
	}
	switch (props.item?.status) {
		case APP_STATUS.running:
			openApp();
			break;
	}
}

watch(
	() => [props.item, userStore.initialized],
	() => {
		if (props.item) {
			updateUI();
		}
	},
	{
		deep: true,
		immediate: true
	}
);

function updateHover(hover: boolean) {
	hoverRef.value = hover;
	updateUI();
}

function updateUI() {
	isDisabled.value = false;
	isLoading.value = false;
	switch (props.item.status) {
		case APP_STATUS.preflightFailed:
			isDisabled.value = true;
			status.value = t('app.get');
			textColor.value = '#B2B0AF';
			backgroundColor.value = '#F6F6F6';
			border.value = '1px solid transparent';
			break;
		case APP_STATUS.uninstalled:
			status.value = t('app.get');
			textColor.value = '#3377FF';
			backgroundColor.value = '#F6F6F6';
			border.value = '1px solid transparent';
			if (!hasCheck && userStore.initialized) {
				userStore.frontendPreflight(props.item, APP_STATUS.uninstalled);
				hasCheck = true;
			}
			break;
		case APP_STATUS.installable:
			status.value = t('app.install');
			textColor.value = '#FFFFFF';
			backgroundColor.value = '#3377FF';
			border.value = '1px solid transparent';
			break;
		case APP_STATUS.pending:
			if (hoverRef.value && props.item?.cfgType !== CFG_TYPE.WORK_FLOW) {
				isLoading.value = false;
				status.value = t('app.cancel');
				textColor.value = '#3377FF';
				backgroundColor.value = '#FFFFFF';
				border.value = '1px solid #3377FF';
			} else {
				isLoading.value = true;
				status.value = '···';
				textColor.value = '#FFFFFF';
				backgroundColor.value = '#3377FF';
				border.value = '1px solid transparent';
			}
			break;
		case APP_STATUS.installing:
			if (hoverRef.value && props.item?.cfgType !== CFG_TYPE.WORK_FLOW) {
				isLoading.value = false;
				status.value = t('app.cancel');
				textColor.value = '#3377FF';
				backgroundColor.value = '#FFFFFF';
				border.value = '1px solid #3377FF';
			} else {
				isLoading.value = true;
				status.value = t('app.installing');
				textColor.value = '#FFFFFF';
				backgroundColor.value = '#3377FF';
				border.value = '1px solid transparent';
			}
			break;
		case APP_STATUS.installed:
			backgroundColor.value = '#3377FF1A';
			textColor.value = '#3377FF';
			border.value = '1px solid transparent';
			status.value = t('app.installed');
			break;
		case APP_STATUS.suspend:
			if (hoverRef.value) {
				status.value = t('app.resume');
			} else {
				backgroundColor.value = '#FEBE011A';
				textColor.value = '#FEBE01';
				border.value = '1px solid transparent';
				status.value = t('app.suspend');
			}
			break;
		case APP_STATUS.waiting:
		case APP_STATUS.resuming:
			isLoading.value = true;
			status.value = '···';
			textColor.value = '#FFFFFF';
			backgroundColor.value = '#3377FF';
			border.value = '1px solid transparent';
			break;
		case APP_STATUS.running:
			backgroundColor.value = '#3377FF1A';
			textColor.value = '#3377FF';
			border.value = '1px solid transparent';
			if (props.isUpdate) {
				status.value = t('app.update');
			} else if (props.item?.cfgType !== CFG_TYPE.APPLICATION) {
				status.value = t('app.running');
			} else {
				status.value = t('app.open');
			}
			break;
		case APP_STATUS.uninstalling:
			textColor.value = '#FFFFFF';
			backgroundColor.value = '#3377FF';
			border.value = '1px solid transparent';
			isLoading.value = true;
			status.value = t('app.uninstalling');
			break;
		case APP_STATUS.upgrading:
			textColor.value = '#FFFFFF';
			backgroundColor.value = '#3377FF';
			border.value = '1px solid transparent';
			isLoading.value = true;
			status.value = t('app.updating');
			break;
		default:
			isDisabled.value = true;
			backgroundColor.value = '#FF4D4D1A';
			textColor.value = '#FF4D4D';
			border.value = '1px solid transparent';
			status.value = props.item?.status;
			break;
	}
}
</script>

<style scoped lang="scss">
.pending-image {
	width: 12px;
	height: 12px;
	animation: animate 1.2s linear infinite;
	-webkit-animation: animate 1.2s linear infinite;
}

.install_btn_bg {
	width: var(--width);
	min-width: var(--width);
	max-width: var(--width);
	border-radius: 4px;
	padding: 0;

	.install_btn_separator_bg {
		width: 1px;
		height: 100%;
		padding-top: 4px;
		padding-bottom: 4px;

		.install_btn_separator {
			width: 1px;
			height: 100%;
			background: #1d5efa33;
		}
	}

	.application_install {
		box-sizing: border-box;
		width: var(--statusWidth);
		min-width: var(--statusWidth);
		max-width: var(--statusWidth);
		color: var(--textColor);
		background: var(--backgroundColor);
		border-radius: 4px var(--radius, 0px) var(--radius, 0px) 4px;
		height: 24px;
		text-overflow: ellipsis;
		white-space: nowrap;
		overflow: hidden;
		font-family: Roboto;
		font-size: 12px;
		font-weight: 500;
		line-height: 16px;
		letter-spacing: 0em;
		text-align: center;
		border: var(--border);
	}

	.application_install_more {
		width: 16px;
		color: var(--textColor);
		background: var(--backgroundColor);
		height: 24px;
		border-radius: 0px 4px 4px 0px;
		gap: 20px;
		text-align: center;
	}

	.application_install_larger {
		box-sizing: border-box;
		width: var(--statusWidth);
		min-width: var(--statusWidth);
		max-width: var(--statusWidth);
		color: var(--textColor);
		background: var(--backgroundColor);
		height: 32px;
		border-radius: 8px var(--radius, 0px) var(--radius, 0px) 8px;
		font-family: Roboto;
		text-overflow: ellipsis;
		white-space: nowrap;
		overflow: hidden;
		font-size: 14px;
		font-weight: 500;
		line-height: 20px;
		letter-spacing: 0em;
		text-align: center;
		border: var(--border);
	}

	.application_install_larger_more {
		width: 20px;
		color: var(--textColor);
		background: var(--backgroundColor);
		height: 32px;
		border-radius: 0px 8px 8px 0px;
		gap: 20px;
		text-align: center;
	}
}
</style>
