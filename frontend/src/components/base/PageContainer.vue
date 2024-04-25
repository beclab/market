<template>
	<div
		class="page-container-root column items-start items-center"
		:class="isAppDetails ? 'app-detail-background' : 'app-normal-background'"
	>
		<div
			class="page-container-title row justify-start"
			:style="{ '--titleHeight': `${titleHeight}px` }"
		>
			<slot name="title" />
		</div>
		<q-scroll-area
			class="page-container-scroll"
			@scroll="onScroll"
			:visible="false"
			:thumb-style="thumbStyle"
			:bar-style="barStyle"
			:style="{
				'--titleHeight': `${titleHeight}px`,
				paddingRight: appStore.isPublic ? '20px' : '0'
			}"
		>
			<div
				class="page-container-scroll-area column justify-center items-center"
			>
				<div
					v-if="!isAppDetails && appStore.isPublic && !hideGradient"
					class="fill1"
				/>
				<div
					v-if="!isAppDetails && appStore.isPublic && !hideGradient"
					class="fill2"
				/>
				<div class="page-container-content">
					<slot name="page" />
				</div>
			</div>
		</q-scroll-area>
	</div>
</template>

<script setup lang="ts">
import { useAppStore } from 'src/stores/app';

const prop = defineProps({
	modelValue: {
		type: Boolean,
		require: false
	},
	verticalPosition: {
		type: Number,
		default: 0
	},
	titleHeight: {
		type: Number,
		default: 0
	},
	isAppDetails: {
		type: Boolean,
		require: false
	},
	hideGradient: {
		type: Boolean,
		require: false
	}
});

const thumbStyle = {
	right: '4px',
	borderRadius: '5px',
	backgroundColor: '#EBEBEB',
	width: '5px',
	opacity: 0.75
};

const barStyle = {
	right: '2px',
	borderRadius: '9px',
	backgroundColor: '#EBEBEB',
	width: '9px',
	opacity: 0.2
};

const appStore = useAppStore();

const emit = defineEmits(['update:modelValue', 'onLoadMore']);
// const slotTitle = useSlots().title

const onScroll = async (info: any) => {
	emit('update:modelValue', info.verticalPosition > prop.verticalPosition);
	if (info.verticalPercentage === 1) {
		emit('onLoadMore');
	}
};
</script>

<style scoped lang="scss">
.page-container-root {
	height: 100%;
	width: 100%;

	.page-container-title {
		height: var(--titleHeight);
		width: 100%;
		max-width: 1920px - 208px;
		min-width: 800px;
	}

	.page-container-scroll {
		width: 100%;
		height: calc(100% - var(--titleHeight));

		.page-container-scroll-area {
			width: 100%;
			height: 100%;
			position: relative;

			.fill1 {
				position: absolute;
				top: -60px;
				right: 14.8%;
				width: 280px;
				height: 280px;
				border-radius: 280px;
				background: $blue-6;
				opacity: 10%;
				filter: blur(80px);
			}

			.fill2 {
				position: absolute;
				top: -43px;
				right: 2%;
				width: 230px;
				height: 230px;
				border-radius: 100px;
				background: $teal-6;
				opacity: 10%;
				filter: blur(80px);
			}
		}

		.page-container-content {
			height: 100%;
			width: 100%;
			max-width: 1920px - 208px;
			min-width: 800px;
		}
	}
}

.app-normal-background {
	background: #ffffff;
}

.app-detail-background {
	background: linear-gradient(
			175.85deg,
			rgba(208, 230, 251, 0) 3.38%,
			rgba(203, 228, 251, 0.04) 42.72%,
			rgba(199, 226, 252, 0.14) 68.45%,
			rgba(163, 211, 255, 0.06) 96.62%
		),
		linear-gradient(
			180deg,
			rgba(228, 245, 255, 0) 0%,
			rgba(208, 238, 255, 0.08) 46.88%,
			rgba(192, 232, 255, 0.24) 70.83%,
			rgba(195, 233, 255, 0.24) 100%
		);
}
</style>
