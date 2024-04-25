<template>
	<div class="row justify-start items-center cursor-pointer">
		<q-icon
			v-if="name"
			size="24px"
			color="primary"
			:name="name"
			style="margin-right: 4px"
		/>
		<div class="icon-label">{{ label }}</div>
	</div>
</template>

<script lang="ts" setup>
defineProps({
	name: String,
	label: String
});
</script>

<style scoped lang="scss">
.icon-label {
	font-family: Roboto;
	font-size: 16px;
	font-weight: 400;
	line-height: 24px;
	letter-spacing: 0;
	text-align: right;
	color: var(--Basic-Info, #37f);
}
</style>
