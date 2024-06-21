<template>
	<div
		class="appstore-body-root column justify-start items-start"
		v-if="loading || showBody"
		:style="{ '--paddingExcludeBody': `${paddingExcludeBody}px` }"
	>
		<div v-if="title" class="app-store-layout row justify-between items-center">
			<div class="app-store-title text-h3 text-ink-1">{{ title }}</div>
			<div class="column justify-between" style="height: 40px">
				<div />
				<div
					v-if="right"
					class="app-store-right text-subtitle2 text-info"
					@click="onRightClick"
				>
					{{ right }}
				</div>
			</div>
		</div>
		<div
			v-if="label"
			class="app-store-layout row justify-between items-center"
			:style="{ '--padding': noLabelPaddingBottom ? '0px' : '12px' }"
		>
			<div class="text-h4 text-ink-1">{{ label }}</div>
			<div
				v-if="right"
				class="app-store-right text-subtitle2 text-info"
				@click="onRightClick"
			>
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
		background: $separator;
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
			padding-bottom: 12px;
		}

		.app-store-right {
			cursor: pointer;
			text-decoration: none;
			text-align: right;
		}
	}
}
</style>
