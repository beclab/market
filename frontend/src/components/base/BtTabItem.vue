<template>
	<div
		class="row justify-center items-center"
		:class="selected ? 'tab-item-root-selected' : 'tab-item-root'"
	>
		<q-icon
			style="margin-right: 4px"
			v-if="icon"
			:name="icon"
			class="font-icon"
			size="16px"
		/>
		<div class="tab-item-title">{{ label }}</div>
	</div>
</template>

<script setup lang="ts">
defineProps({
	selected: {
		type: Boolean,
		default: false
	},
	label: {
		type: String,
		default: '',
		require: false
	},
	icon: {
		type: String,
		default: '',
		require: false
	}
});
</script>

<style scoped lang="scss">
.tab-item-root {
	height: 32px;
	width: 100px;
	padding-left: 12px;
	padding-right: 12px;
	color: $sub-title;
	text-align: center;
	border-radius: 8px;
}

.tab-item-root-selected {
	height: 32px;
	width: 100px;
	padding-left: 12px;
	padding-right: 12px;
	color: $main-style;
	border-radius: 8px;
	text-align: center;
	background-color: rgba(51, 119, 255, 0.1);
}

.tab-item-title {
	font-family: Roboto;
	font-size: 12px;
	font-weight: 500;
	line-height: 16px;
	letter-spacing: 0em;
	text-align: center;
	text-transform: capitalize;
}
</style>
