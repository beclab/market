<template>
	<div>
		<div
			class="expend-text-style"
			:class="showDescription ? 'multi-row' : 'break-all'"
			:style="{ '--displayLine': displayLine }"
			ref="multiRow"
			v-html="formattedDescription"
		/>
		<span
			v-if="needMore"
			class="expend-text-style text-blue-6 cursor-pointer"
			@click="toggleDescription"
		>
			{{ showDescription ? lessText : moreText }}
		</span>
	</div>
</template>

<script lang="ts" setup>
import { onMounted, computed, ref, watch } from 'vue';

const props = defineProps({
	text: {
		type: String,
		default: '',
		require: true
	},
	displayLine: {
		type: Number,
		default: 3
	},
	moreText: {
		type: String,
		default: 'More',
		require: false
	},
	lessText: {
		type: String,
		default: '',
		require: false
	}
});

const showDescription = ref(false);
const needMore = ref(false);

const formattedDescription = computed(() => {
	return props.text.replace(/(\r\n|\n|\r)/gm, '<br/>');
});

const multiRow = ref<HTMLElement | null>(null);

const checkTextOverflow = () => {
	if (multiRow.value) {
		const height = multiRow.value.scrollHeight;
		const lineHeight = parseInt(getComputedStyle(multiRow.value).lineHeight);

		needMore.value = height > lineHeight * props.displayLine;
	}
};

const toggleDescription = () => {
	showDescription.value = !showDescription.value;
	checkTextOverflow();
};

onMounted(() => {
	checkTextOverflow();
});

watch(formattedDescription, () => {
	checkTextOverflow();
});
</script>

<style scoped lang="scss">
.expend-text-style {
	font-family: Roboto;
	font-size: 14px;
	font-weight: 400;
	line-height: 20px;
	letter-spacing: 0em;
	text-align: left;
	color: $sub-title;
}

.multi-row {
	overflow: visible;
}

.break-all {
	display: -webkit-box;
	overflow: hidden;
	white-space: normal !important;
	text-overflow: ellipsis;
	/* autoprefixer: off */
	word-wrap: break-word;
	-webkit-line-clamp: var(--displayLine);
	-webkit-box-orient: vertical;
}
</style>
