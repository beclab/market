<template>
	<div
		v-if="skeleton"
		:class="larger ? 'application_single_larger' : 'application_single_normal'"
		:style="{
			borderBottom: isLastLine ? 'none' : `1px solid ${separator}`,
			'--iconSize': showIcon(type)
				? larger
					? '100px'
					: '64px'
				: larger
					? '224px'
					: '128px',
			'--paddingLeft': showIcon(type)
				? larger
					? '20px'
					: '12px'
				: larger
					? '32px'
					: '20px',
			'--paddingTop': showIcon(type)
				? larger
					? '20px'
					: '20px'
				: larger
					? '8px'
					: '0',
			'--paddingBottom': showIcon(type)
				? larger
					? '20px'
					: '20px'
				: larger
					? '8px'
					: '0'
		}"
		class="row justify-between items-center"
	>
		<app-icon
			v-if="showIcon(type)"
			:skeleton="true"
			:size="larger ? 100 : 64"
		/>

		<app-featured-image v-else :width="larger ? 224 : 128" :skeleton="true" />

		<div class="application_right column justify-center items-start">
			<div class="application_title_layout row justify-start items-center">
				<q-skeleton
					:width="larger ? '75px' : '60px'"
					:height="larger ? '26px' : '22px'"
				/>
				<q-skeleton
					v-if="version"
					class="application_version"
					width="41px"
					height="20px"
				/>
			</div>

			<q-skeleton
				class="application_content q-mt-xs"
				:width="larger ? '500px' : '130px'"
				:height="larger ? '20px' : '16px'"
			/>
			<q-skeleton
				v-if="!appStore.isPublic"
				class="application_btn"
				:width="larger ? '88px' : '72px'"
				:height="larger ? '30px' : '22px'"
			/>
		</div>
	</div>
	<div
		v-else
		:class="
			larger
				? 'application_single_larger'
				: 'application_single_normal cursor-pointer'
		"
		:style="{
			borderBottom: isLastLine ? 'none' : `1px solid ${separator}`,
			'--iconSize': showIcon(type)
				? larger
					? '100px'
					: '64px'
				: larger
					? '224px'
					: '128px',
			'--paddingLeft': showIcon(type)
				? larger
					? '20px'
					: '12px'
				: larger
					? '32px'
					: '20px',
			'--paddingTop': showIcon(type)
				? larger
					? '20px'
					: '20px'
				: larger
					? '8px'
					: '0',
			'--paddingBottom': showIcon(type)
				? larger
					? '20px'
					: '20px'
				: larger
					? '8px'
					: '0'
		}"
		class="row justify-between items-center"
		@click="goAppDetails"
		@mouseover="hoverRef = true"
		@mouseleave="hoverRef = false"
	>
		<app-icon
			v-if="showIcon(type)"
			:src="item.icon"
			:size="larger ? 100 : 64"
			:cs-app="clusterScopedApp"
		/>

		<app-featured-image
			v-else
			:width="larger ? 224 : 128"
			:src="item.featuredImage ? item.featuredImage : item.icon"
		/>

		<div class="application_right column justify-center items-start">
			<div class="application_title_layout row justify-start items-baseline">
				<div class="application_title text-ink-1">
					{{ getAppFieldI18n(item, APP_FIELD.TITLE) }}
				</div>
				<div v-if="version" class="application_version">
					{{ item.versionName }}
				</div>
			</div>

			<div
				class="text-ink-3"
				:class="
					appStore.isPublic
						? 'application_content_public'
						: 'application_content'
				"
			>
				{{ getAppFieldI18n(item, APP_FIELD.DESCRIPTION) }}
			</div>
			<span
				v-if="!appStore.isPublic"
				class="application_btn row justify-start items-center"
			>
				<install-button
					v-if="item"
					:development="development"
					:item="item"
					:larger="larger"
					:is-update="isUpdate"
					:manager="manager"
				/>
				<div
					v-if="clusterScopedApp"
					class="application_cluster_scoped text-ink-3"
				>
					Cluster-Scoped
				</div>
			</span>

			<span
				v-if="appStore.isPublic && larger"
				class="text-body3 text-grey-5 q-mt-md"
				>{{ t('main.terminus') }}
			</span>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { onMounted, PropType, ref } from 'vue';
import {
	AppStoreInfo,
	getAppFieldI18n,
	APP_FIELD,
	TRANSACTION_PAGE
} from 'src/constants/constants';
import InstallButton from 'components/appcard/InstallButton.vue';
import { useRouter } from 'vue-router';
import { useAppStore } from 'src/stores/app';
import AppIcon from 'components/appcard/AppIcon.vue';
import AppFeaturedImage from 'components/appcard/AppFeaturedImage.vue';
import { useI18n } from 'vue-i18n';
import { useColor } from '@bytetrade/ui';
import { CFG_TYPE, showIcon } from 'src/constants/config';

const router = useRouter();
const appStore = useAppStore();
const hoverRef = ref(false);
const { t } = useI18n();

const props = defineProps({
	item: {
		type: Object as PropType<AppStoreInfo>,
		required: false
	},
	type: {
		type: String,
		default: CFG_TYPE.APPLICATION
	},
	development: {
		type: Boolean,
		required: false
	},
	version: {
		type: Boolean,
		default: false
	},
	isLastLine: {
		type: Boolean,
		default: false
	},
	disabled: {
		type: Boolean,
		default: false
	},
	larger: {
		type: Boolean,
		default: false
	},
	isUpdate: {
		type: Boolean,
		required: false,
		default: false
	},
	manager: {
		type: Boolean,
		required: false,
		default: false
	},
	skeleton: {
		type: Boolean,
		default: false
	}
});

const clusterScopedApp = ref(false);
const { color: separator } = useColor('separator');

onMounted(() => {
	if (props.item && props.item.options && props.item.options.appScope) {
		clusterScopedApp.value = props.item.options.appScope.clusterScoped;
	} else {
		clusterScopedApp.value = false;
	}
});

function goAppDetails() {
	console.log('click app');
	if (props.disabled) {
		return;
	}
	if (!props.item) {
		console.log('go app details failure');
		return;
	}
	appStore.setAppItem(props.item);
	router.push({
		name: TRANSACTION_PAGE.App,
		params: {
			name: props.item.name,
			type: props.item.cfgType
		}
	});
}
</script>
<style lang="scss" scoped>
.application_single_normal {
	width: 100%;
	height: 113px;
	overflow: hidden;

	.application_right {
		width: calc(100% - var(--iconSize));
		height: 100%;
		padding-top: var(--paddingTop);
		padding-bottom: var(--paddingBottom);
		padding-left: var(--paddingLeft);

		.application_title_layout {
			width: 100%;
			max-height: 40px;
			text-overflow: ellipsis;
			white-space: nowrap;
			overflow: hidden;

			.application_title {
				font-family: Roboto;
				font-size: 16px;
				font-weight: 700;
				line-height: 24px;
				letter-spacing: 0;
				text-align: left;
				max-width: 70%;
			}

			.application_version {
				font-family: Roboto;
				font-size: 14px;
				font-weight: 700;
				line-height: 20px;
				letter-spacing: 0em;
				text-align: left;
				color: $blue;
				max-width: 25%;
				margin-left: 8px;
			}
		}

		.application_content {
			font-family: Roboto;
			font-size: 12px;
			font-weight: 400;
			line-height: 16px;
			letter-spacing: 0em;
			text-align: left;
			overflow: hidden;
			text-overflow: ellipsis;
			display: -webkit-box;
			-webkit-line-clamp: 1;
			-webkit-box-orient: vertical;
		}

		.application_content_public {
			@extend .application_content;
			-webkit-line-clamp: 1;
		}

		.application_btn {
			margin-top: 8px;

			.application_cluster_scoped {
				width: calc(100% - 80px);
				font-family: Roboto;
				margin-left: 8px;
				font-size: 10px;
				font-weight: 400;
				line-height: 12px;
				letter-spacing: 0em;
				text-align: left;
				text-overflow: ellipsis;
				white-space: nowrap;
				overflow: hidden;
			}
		}
	}
}

.application_single_larger {
	width: 100%;
	height: 140px;
	overflow: hidden;

	.application_right {
		width: calc(100% - var(--iconSize));
		height: 100%;
		padding-top: var(--paddingTop);
		padding-bottom: var(--paddingBottom);
		padding-left: var(--paddingLeft);
		position: relative;

		.application_title_layout {
			width: 100%;
			text-overflow: ellipsis;
			white-space: nowrap;
			overflow: hidden;

			.application_title {
				font-family: Roboto;
				font-size: 20px;
				font-weight: 600;
				line-height: 28px;
				letter-spacing: 0em;
				text-align: left;
				max-width: 70%;
			}

			.application_version {
				font-family: Roboto;
				font-size: 14px;
				font-weight: 500;
				line-height: 20px;
				letter-spacing: 0em;
				text-align: left;
				max-width: 25%;
				margin-left: 8px;
			}
		}

		.application_content {
			font-family: Roboto;
			font-size: 14px;
			font-weight: 400;
			line-height: 20px;
			letter-spacing: 0em;
			text-align: left;
			overflow: hidden;
			text-overflow: ellipsis;
			display: -webkit-box;
			-webkit-line-clamp: 2;
			-webkit-box-orient: vertical;
		}

		.application_btn {
			margin-top: 20px;

			.application_cluster_scoped {
				width: calc(100% - 96px);
				font-family: Roboto;
				margin-left: 8px;
				text-overflow: ellipsis;
				white-space: nowrap;
				overflow: hidden;
				font-size: 10px;
				font-weight: 400;
				line-height: 12px;
				letter-spacing: 0em;
				text-align: left;
			}
		}
	}
}
</style>
