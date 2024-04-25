<template>
	<div
		class="appstore-body-root column justify-start items-start"
		v-if="loading || showBody"
		:style="{ '--paddingExcludeBody': `${paddingExcludeBody}px` }"
	>
		<div v-if="title" class="app-store-layout row justify-between items-center">
			<div class="app-store-title">{{ title }}</div>
			<div class="column justify-between" style="height: 40px">
				<div />
				<div v-if="right" class="app-store-right" @click="onRightClick">
					{{ right }}
				</div>
			</div>
		</div>
		<div
			v-if="label"
			class="app-store-layout row justify-between items-center"
			:style="{ '--padding': noLabelPaddingBottom ? '0px' : '12px' }"
		>
			<div class="app-store-label">{{ label }}</div>
			<div v-if="right" class="app-store-right" @click="onRightClick">
				{{ right }}
			</div>
		</div>
		<q-separator v-if="titleSeparator" class="app-store-separator" />
		<div
			v-if="bodySlot"
			:style="{
				width: '100%',
				marginTop: `${bodyMarginTop}px`,
				marginBottom: `${bodyMarginBottom}px`
			}"
		>
			<slot v-if="loading" name="loading" />
			<slot v-else-if="showBody" name="body" />
		</div>
		<q-separator v-if="bottomSeparator" class="app-store-separator" />
	</div>
</template>

<script lang="ts" setup>
// import {PropType} from 'vue';

import { useSlots } from 'vue';

defineProps({
	title: String,
	label: String,
	right: String,
	loading: {
		type: Boolean,
		default: false
	},
	showBody: {
		type: Boolean,
		default: true
	},
	bodyMarginTop: {
		type: Number,
		default: 0
	},
	paddingExcludeBody: {
		type: Number,
		default: 0
	},
	bodyMarginBottom: {
		type: Number,
		default: 0
	},
	titleSeparator: {
		type: Boolean,
		default: false
	},
	bottomSeparator: {
		type: Boolean,
		default: false
	},
	noLabelPaddingBottom: {
		type: Boolean,
		default: false
	}
});

const emit = defineEmits(['onRightClick']);

const onRightClick = () => {
	emit('onRightClick');
};

const bodySlot = !!useSlots().body;
</script>

<style scoped lang="scss">
.appstore-body-root {
	width: 100%;
	height: auto;

	.app-store-separator {
		width: calc(100% - var(--paddingExcludeBody) - var(--paddingExcludeBody));
		background: #ebebeb;
		margin-left: var(--paddingExcludeBody);
		margin-right: var(--paddingExcludeBody);
		height: 1px;
	}

	.app-store-layout {
		width: 100%;
		padding: 12px var(--paddingExcludeBody) var(--padding);

		.app-store-title {
			padding-left: var(--paddingExcludeBody);
			padding-right: var(--paddingExcludeBody);
			font-family: Roboto;
			font-size: 32px;
			font-weight: 700;
			line-height: 40px;
			letter-spacing: 0em;
			text-align: left;
			color: $title;
			padding-bottom: 12px;
		}

		.app-store-label {
			font-family: Roboto;
			font-size: 24px;
			font-weight: 600;
			line-height: 32px;
			letter-spacing: 0em;
			text-align: left;
			color: $title;
		}

		.app-store-right {
			font-family: Roboto;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			font-weight: 500;
			line-height: 20px;
			letter-spacing: 0em;
			text-align: right;
			color: $blue;
		}
	}
}
</style>
