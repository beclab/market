<template>
	<transition name="fade">
		<div
			v-if="showHeaderBar && item"
			class="row justify-between items-center application-details-bar"
			:style="absolute ? 'position: absolute;z-index: 99999' : ''"
		>
			<div class="row justify-start items-center">
				<q-icon
					class="application_bar_return cursor-pointer"
					name="sym_r_arrow_back_ios_new"
					size="20px"
					@click="clickReturn"
				/>
				<q-img v-if="showIcon" class="application_bar_img" :src="item.icon">
					<template v-slot:loading>
						<q-skeleton width="32px" height="32px" />
					</template>
				</q-img>
				<div class="application_bar_name text-color-title">
					{{ item.title }}
				</div>
				<div class="application_bar_version">{{ item.versionName }}</div>
			</div>
			<install-button
				v-if="showInstallBtn"
				:item="item"
				:development="false"
				:larger="true"
				:version="true"
			/>
		</div>
	</transition>
</template>

<script lang="ts" setup>
import InstallButton from 'components/appcard/InstallButton.vue';
import { PropType } from 'vue';
import { AppStoreInfo } from 'src/constants/constants';
import { useRouter } from 'vue-router';
import { useAppStore } from 'src/stores/app';

const props = defineProps({
	item: {
		type: Object as PropType<AppStoreInfo>,
		required: true
	},
	showHeaderBar: {
		type: Boolean,
		default: false,
		required: false
	},
	showIcon: {
		type: Boolean,
		default: true
	},
	showInstallBtn: {
		type: Boolean,
		default: false,
		required: false
	},
	absolute: {
		type: Boolean,
		default: false
	}
});

const router = useRouter();
const appStore = useAppStore();

const clickReturn = () => {
	if (window.history && window.history.state && !window.history.state.back) {
		router.replace('/');
		return;
	}
	appStore.removeAppItem(props.item.name);
	router.back();
};
</script>

<style scoped lang="scss">
.application-details-bar {
	height: 56px;
	width: 100%;
	background-color: white;
	padding-right: 44px;
	box-shadow: 0px 2px 4px 0px #0000001a;

	.application_bar_return {
		margin-left: 18px;
	}

	.application_bar_img {
		width: 32px;
		height: 32px;
		margin-left: 6px;
		border-radius: 8px;
		box-shadow: 0px 2px 4px 0px #0000001a;
	}

	.application_bar_name {
		font-family: Roboto;
		font-size: 16px;
		font-weight: 500;
		line-height: 24px;
		letter-spacing: 0em;
		text-align: left;
		margin-left: 12px;
	}

	.application_bar_version {
		font-family: Roboto;
		font-size: 14px;
		font-weight: 500;
		line-height: 20px;
		letter-spacing: 0em;
		text-align: center;
		margin-left: 8px;
		color: #3377ff;
	}
}
</style>
