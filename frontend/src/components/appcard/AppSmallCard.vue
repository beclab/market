<template>
	<div
		v-if="skeleton"
		class="app-small-card row justify-between items-center"
		:style="{ borderBottom: isLastLine ? 'none' : '1px solid #EBEBEB' }"
	>
		<app-icon :skeleton="true" :size="56" />
		<div class="app-small-card__right row justify-between items-center">
			<div class="app-small-card__right__text column justify-center">
				<q-skeleton width="60px" height="20px" />
				<q-skeleton width="100px" height="16px" style="margin-top: 4px" />
			</div>
			<q-skeleton width="72px" height="24px" />
		</div>
	</div>
	<div
		v-else-if="item"
		class="cursor-pointer app-small-card row justify-between items-center"
		@click="goAppDetails"
		:style="{
			borderBottom: isLastLine ? '1px solid transparent' : '1px solid #EBEBEB'
		}"
	>
		<app-icon :src="item.icon" :size="56" :cs-app="clusterScopedApp" />
		<div class="app-small-card__right row justify-between items-center">
			<div
				class="app-small-card__right__text column justify-center"
				:style="
					appStore.isPublic
						? 'width: calc(100% - 22px);'
						: 'width: calc(100% - 72px - 22px);'
				"
			>
				<div class="app-small-card__right__text__title text-color-title">
					{{ item.title }}
				</div>
				<div class="app-small-card__right__text__content text-color-subTitle">
					{{ item.desc }}
				</div>
			</div>
			<install-button v-if="!appStore.isPublic" :item="item" />
			<div
				v-if="clusterScopedApp"
				class="app-small-card__right__cluster_scoped"
			>
				Cluster-Scoped
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { onMounted, PropType, ref } from 'vue';
import { AppStoreInfo, TRANSACTION_PAGE } from 'src/constants/constants';
import InstallButton from 'src/components/appcard/InstallButton.vue';
import { useRouter } from 'vue-router';
import { useAppStore } from 'src/stores/app';
import AppIcon from 'src/components/appcard/AppIcon.vue';

const router = useRouter();
const appStore = useAppStore();
const props = defineProps({
	item: {
		type: Object as PropType<AppStoreInfo>,
		required: false
	},
	disabled: {
		type: Boolean,
		default: false
	},
	isLastLine: {
		type: Boolean,
		default: false
	},
	skeleton: {
		type: Boolean,
		default: false
	}
});

const clusterScopedApp = ref(false);

onMounted(() => {
	if (props.item && props.item.options && props.item.options.appScope) {
		clusterScopedApp.value = props.item.options.appScope.clusterScoped;
	} else {
		clusterScopedApp.value = false;
	}
});

function goAppDetails() {
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

defineExpose({ goAppDetails });
</script>
<style lang="scss" scoped>
.app-small-card {
	width: 100%;
	height: 80px;
	overflow: hidden;

	&__right {
		width: calc(100% - 56px);
		height: 100%;
		position: relative;

		&__text {
			height: 56px;
			padding-right: 4px;
			margin-left: 8px;

			&__title {
				font-family: Roboto;
				font-size: 14px;
				font-weight: 500;
				line-height: 20px;
				width: 100%;
				letter-spacing: 0em;
				text-align: left;
				overflow: hidden;
				text-overflow: ellipsis;
				display: -webkit-box;
				-webkit-line-clamp: 1;
				-webkit-box-orient: vertical;
			}

			&__content {
				font-family: Roboto;
				width: 100%;
				font-size: 12px;
				font-weight: 400;
				line-height: 16px;
				letter-spacing: 0em;
				text-align: left;
				overflow: hidden;
				text-overflow: ellipsis;
				display: -webkit-box;
				-webkit-line-clamp: 2;
				-webkit-box-orient: vertical;
			}
		}

		&__cluster_scoped {
			width: 72px;
			font-family: Roboto;
			position: absolute;
			bottom: 12px;
			right: 0;
			text-overflow: ellipsis;
			white-space: nowrap;
			overflow: hidden;
			font-size: 8px;
			font-weight: 400;
			line-height: 12px;
			letter-spacing: 0em;
			text-align: center;
			color: var(--Grey-05, #adadad);
		}
	}
}
</style>
