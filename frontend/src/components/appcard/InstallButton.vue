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
					? 'calc(100% - 25px)'
					: '100%'
				: showDropMenu
					? 'calc(100% - 21px)'
					: '100%'
		}"
	>
		<q-btn
			:loading="isLoading"
			:class="larger ? 'application_install_larger' : 'application_install'"
			:style="{
				'--radius': showDropMenu ? '0' : larger ? '8px' : '4px'
			}"
			@click="onClick"
			@mouseover="updateHover(true)"
			@mouseleave="updateHover(false)"
			dense
			flat
			no-caps
		>
			<div>{{ status }}</div>

			<template v-slot:loading>
				<div
					style="width: 100%; height: 100%"
					class="row justify-center items-center"
					v-if="showAppStatus(item)"
				>
					{{ status }}
				</div>
				<progress-button
					ref="progressBar"
					v-if="showDownloadProgress(item)"
					:progress="item.progress"
					:covered-text-color="white"
					:default-text-color="blueDefault"
					:progress-bar-color="blueDefault"
				/>
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
			:style="{
				background: backgroundColor,
				height: larger ? '32px' : '24px',
				'--paddingY': larger ? '8px' : '6px'
			}"
		>
			<div class="install_btn_separator" />
		</div>
		<q-btn-dropdown
			v-if="showDropMenu"
			dropdown-icon="img:/arrow.svg"
			:size="larger ? '12px' : '9px'"
			:class="
				larger ? 'application_install_larger_more' : 'application_install_more'
			"
			content-class="dropdown-menu"
			flat
			dense
			:menu-offset="[0, 4]"
		>
			<div class="column text-body3">
				<div
					v-if="canLoad(item)"
					class="dropdown-menu-item q-mt-xs"
					v-close-popup
					@click="onLoad"
				>
					{{ t('app.load') }}
				</div>
				<div
					v-if="canUnload(item)"
					class="dropdown-menu-item q-mt-xs"
					v-close-popup
					@click="onUnload"
				>
					{{ t('app.unload') }}
				</div>
				<div
					v-if="canResume(item)"
					class="dropdown-menu-item q-mt-xs"
					v-close-popup
					@click="onResume"
				>
					{{ t('app.resume') }}
				</div>
				<div
					v-if="canSuspend(item)"
					class="dropdown-menu-item q-mt-xs"
					v-close-popup
					@click="onSuspend"
				>
					{{ t('app.suspend') }}
				</div>
				<div
					v-if="isUpdate"
					class="dropdown-menu-item q-mt-xs"
					v-close-popup
					@click="onUpdateOpen"
				>
					{{ t('app.open') }}
				</div>
				<div class="dropdown-menu-item" v-close-popup @click="onUninstall">
					{{ t('app.uninstall') }}
				</div>
			</div>
		</q-btn-dropdown>
	</div>
</template>

<script lang="ts" setup>
import { computed, onBeforeMount, onMounted, PropType, ref, watch } from 'vue';
import { APP_STATUS, AppStoreInfo } from 'src/constants/constants';
import { useAppStore } from 'src/stores/app';
import { openApplication } from 'src/api/private/operations';
import { getRequireImage } from 'src/utils/imageUtils';
import { useI18n } from 'vue-i18n';
import { useUserStore } from 'src/stores/user';
import { BtDialog, useColor } from '@bytetrade/ui';
import {
	canInstallingCancel,
	canLoad,
	canOpen,
	canResume,
	canSuspend,
	canUnload,
	showAppStatus,
	showDownloadProgress
} from 'src/constants/config';
import { bus, BUS_EVENT } from 'src/utils/bus';
import ProgressButton from 'src/components/base/ProgressButton.vue';
import DependencyDialog from 'src/components/appintro/DependencyDialog.vue';
import { useSettingStore } from 'src/stores/setting';
import { useQuasar } from 'quasar';

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
const isDisabled = ref(false);
const isLoading = ref<boolean>(false);
const hoverRef = ref(false);
const textColor = ref<string>();
const backgroundColor = ref<string>();
const border = ref<string>();
const { t } = useI18n();
const $q = useQuasar();
const status = ref();
let hasCheck = false;

const showDropMenu = computed(() => {
	return (
		props.manager &&
		(props.item.status === APP_STATUS.running ||
			props.item.status === APP_STATUS.suspend ||
			props.item.status === APP_STATUS.installed)
	);
});
const { color: blueDefault } = useColor('blue-default');
const { color: grey } = useColor('background-3');
const { color: ink3 } = useColor('ink-3');
const { color: white } = useColor('ink-on-brand');
const { color: background1 } = useColor('background-1');
const { color: blueAlpha } = useColor('blue-alpha');
const { color: redAlpha } = useColor('red-alpha');
const { color: negative } = useColor('negative');
// const { color: orangeDefault } = useColor('orange-default');
// const { color: orangeSoft } = useColor('orange-soft');
const settingStore = useSettingStore();

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
			if (settingStore.hasDependency(props.item)) {
				console.log(1);
				settingStore.dependencyShow = true;
				$q.dialog({
					component: DependencyDialog,
					componentProps: {
						app: props.item
					}
				})
					.onOk(() => {
						//Do Something
					})
					.onDismiss(() => {
						settingStore.dependencyShow = false;
					});
			}
			console.log(2);
			break;
		case APP_STATUS.pending:
		case APP_STATUS.downloading:
		case APP_STATUS.installing:
		case APP_STATUS.initializing:
			console.log(props.item?.name);
			console.log('cancel installing');
			if (canInstallingCancel(props.item?.cfgType)) {
				appStore.cancelInstallingApp(props.item, props.development);
			}
			break;
		case APP_STATUS.suspend:
			if (props.isUpdate) {
				appStore.upgradeApp(props.item);
			}
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

	if (canOpen(props.item)) {
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

		if (entrance.state === 'crash') {
			onSuspendTips(true);
		} else if (entrance.state === 'suspend') {
			onSuspendTips(false);
		} else {
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

async function onResume() {
	if (!props.item) {
		return;
	}

	switch (props.item?.status) {
		case APP_STATUS.suspend:
			appStore.resumeApp(props.item, props.development);
			break;
	}
}

async function onSuspend() {
	if (!props.item) {
		return;
	}

	switch (props.item?.status) {
		case APP_STATUS.running:
			appStore.suspendApp(props.item, props.development);
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

const callBack = (name) => {
	if (props.item && name && name === props.item.name) {
		hasCheck = false;
		updateUI();
	}
};

onMounted(() => {
	bus.on(BUS_EVENT.UPDATE_APP_DEPENDENCIES, callBack);
});

onBeforeMount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_DEPENDENCIES, callBack);
});

async function onUninstall() {
	if (!props.item) {
		return;
	}
	switch (props.item?.status) {
		case APP_STATUS.running:
		case APP_STATUS.suspend:
		case APP_STATUS.installed:
			BtDialog.show({
				title: t('app.uninstall'),
				message: t('my.sure_to_uninstall_the_app', { title: props.item.title }),
				okStyle: {
					background: blueDefault.value,
					color: white.value
				},
				okText: t('base.confirm'),
				cancel: true
			})
				.then((res) => {
					if (res) {
						console.log('click ok');
						appStore.uninstallApp(props.item, props.development);
					} else {
						console.log('click cancel');
					}
				})
				.catch((err) => {
					console.log('click error', err);
				});
			break;
	}
}

async function onSuspendTips(isCrash: boolean) {
	if (!props.item) {
		return;
	}
	switch (props.item?.status) {
		case APP_STATUS.suspend:
			BtDialog.show({
				title: isCrash ? t('app.crash') : t('app.suspend'),
				message: isCrash
					? t('my.application_has_crashed')
					: t('my.application_has_been_suspended'),
				okStyle: {
					background: blueDefault.value,
					color: white.value
				},
				okText: t('base.ok'),
				cancel: false
			})
				.then((res) => {
					if (res) {
						console.log('click ok');
					} else {
						console.log('click cancel');
					}
				})
				.catch((err) => {
					console.log('click error', err);
				});
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
			textColor.value = ink3.value;
			backgroundColor.value = grey.value;
			border.value = '1px solid transparent';
			if (!hasCheck && userStore.initialized) {
				userStore.frontendPreflight(props.item, APP_STATUS.uninstalled);
				hasCheck = true;
			}
			break;
		case APP_STATUS.uninstalled:
			status.value = t('app.get');
			textColor.value = blueDefault.value;
			backgroundColor.value = grey.value;
			border.value = '1px solid transparent';
			if (!hasCheck && userStore.initialized) {
				userStore.frontendPreflight(props.item, APP_STATUS.uninstalled);
				hasCheck = true;
			}
			break;
		case APP_STATUS.installable:
			status.value = t('app.install');
			textColor.value = white.value;
			backgroundColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			break;
		case APP_STATUS.pending:
			if (hoverRef.value && canInstallingCancel(props.item?.cfgType)) {
				isLoading.value = false;
				status.value = t('app.cancel');
				textColor.value = blueDefault.value;
				backgroundColor.value = background1.value;
				border.value = `1px solid ${blueDefault.value}`;
			} else {
				isLoading.value = true;
				textColor.value = white.value;
				backgroundColor.value = blueDefault.value;
				border.value = '1px solid transparent';
			}
			break;
		case APP_STATUS.installing:
			if (hoverRef.value && canInstallingCancel(props.item?.cfgType)) {
				isLoading.value = false;
				status.value = t('app.cancel');
				textColor.value = blueDefault.value;
				backgroundColor.value = background1.value;
				border.value = `1px solid ${blueDefault.value}`;
			} else {
				isLoading.value = true;
				status.value = t('app.installing');
				textColor.value = white.value;
				backgroundColor.value = blueDefault.value;
				border.value = '1px solid transparent';
			}
			break;
		case APP_STATUS.downloading:
			if (hoverRef.value && canInstallingCancel(props.item?.cfgType)) {
				isLoading.value = false;
				status.value = t('app.cancel');
				textColor.value = blueDefault.value;
				backgroundColor.value = background1.value;
				border.value = `1px solid ${blueDefault.value}`;
			} else {
				isLoading.value = true;
				status.value = t('app.installing');
				textColor.value = white.value;
				backgroundColor.value = blueAlpha.value;
				border.value = '1px solid transparent';
			}
			break;
		case APP_STATUS.initializing:
			if (hoverRef.value && canInstallingCancel(props.item?.cfgType)) {
				isLoading.value = false;
				status.value = t('app.cancel');
				textColor.value = blueDefault.value;
				backgroundColor.value = background1.value;
				border.value = `1px solid ${blueDefault.value}`;
			} else {
				isLoading.value = true;
				status.value = t('app.initializing');
				textColor.value = white.value;
				backgroundColor.value = blueDefault.value;
				border.value = '1px solid transparent';
			}
			break;
		case APP_STATUS.installed:
			backgroundColor.value = blueAlpha.value;
			textColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			status.value = t('app.installed');
			break;
		case APP_STATUS.resuming:
		case APP_STATUS.waiting:
			isLoading.value = true;
			status.value = '';
			textColor.value = white.value;
			backgroundColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			break;
		case APP_STATUS.suspend:
			backgroundColor.value = blueAlpha.value;
			textColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			if (props.isUpdate) {
				status.value = t('app.update');
			} else {
				status.value = t('app.suspend');
			}
			break;
		case APP_STATUS.running:
			backgroundColor.value = blueAlpha.value;
			textColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			if (props.isUpdate) {
				status.value = t('app.update');
			} else if (canOpen(props.item)) {
				status.value = t('app.open');
			} else {
				status.value = t('app.running');
			}
			break;
		case APP_STATUS.uninstalling:
			textColor.value = white.value;
			backgroundColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			isLoading.value = true;
			status.value = t('app.uninstalling');
			break;
		case APP_STATUS.upgrading:
			textColor.value = white.value;
			backgroundColor.value = blueDefault.value;
			border.value = '1px solid transparent';
			isLoading.value = true;
			status.value = t('app.updating');
			break;
		default:
			isDisabled.value = true;
			backgroundColor.value = redAlpha.value;
			textColor.value = negative.value;
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
		width: 5px;
		height: 100%;
		padding: var(--paddingY) 2px;

		.install_btn_separator {
			width: 1px;
			height: 100%;
			background: $btn-stroke;
		}
	}

	.application_install {
		box-sizing: border-box;
		width: var(--statusWidth);
		min-width: var(--statusWidth);
		max-width: var(--statusWidth);
		color: var(--textColor);
		background: var(--backgroundColor);
		border-radius: 4px var(--radius, 0) var(--radius, 0) 4px !important;
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
		border-radius: 0 4px 4px 0 !important;
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
		border-radius: 8px var(--radius, 0) var(--radius, 0) 8px !important;
		font-family: Roboto;
		text-overflow: ellipsis;
		white-space: nowrap;
		overflow: hidden;
		font-size: 14px;
		font-weight: 500;
		line-height: 20px;
		letter-spacing: 0;
		text-align: center;
		border: var(--border);
	}

	.application_install_larger_more {
		width: 20px;
		color: var(--textColor);
		background: var(--backgroundColor);
		height: 32px;
		border-radius: 0 8px 8px 0 !important;
		gap: 20px;
		text-align: center;
	}
}

.dropdown-menu-item {
	height: 32px;
	color: $ink-2;
	padding: 8px 12px;

	&:hover {
		background: $background-hover;
		border-radius: 4px;
	}

	&:active {
		background: $background-hover;
		border-radius: 4px;
	}
}
</style>
