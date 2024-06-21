<template>
	<div v-if="skeleton" class="version-record-root column justify-start">
		<div class="row justify-between items-center">
			<q-skeleton width="50px" height="20px" />
			<q-skeleton width="40px" height="20px" />
		</div>
		<q-skeleton class="version-record-desc" width="200px" height="20px" />
		<q-skeleton style="margin-top: 2px" width="300px" height="20px" />
	</div>
	<div v-else class="version-record-root q-py-lg column justify-start">
		<div class="row justify-between items-center">
			<div class="text-h6 text-ink-1">{{ record.version }}</div>
			<div class="text-body2 text-ink-3">
				{{ formatTimeDifference(record.mergedAt) }}
			</div>
		</div>

		<expend-text-view
			:display-line="2"
			class="q-mt-md"
			:text="record.upgradeDescription"
		/>
	</div>
</template>

<script lang="ts" setup>
import { PropType } from 'vue';
import { VersionRecord } from 'src/constants/constants';
import ExpendTextView from 'components/appintro/ExpendTextView.vue';
import { formatTimeDifference } from 'src/utils/utils';

defineProps({
	record: {
		type: Object as PropType<VersionRecord>,
		required: true
	},
	skeleton: {
		type: Boolean,
		default: false
	}
});
</script>

<style scoped lang="scss">
.version-record-root {
	width: 100%;
	height: auto;
	border-bottom: 1px solid $separator;
}
</style>
