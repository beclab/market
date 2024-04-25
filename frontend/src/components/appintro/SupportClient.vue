<template>
	<div v-if="value" class="support-client row justify-between items-center">
		<div class="row justify-start items-center">
			<q-img class="client-icon" :src="imageRef" />
			<div class="info-label">{{ label }}</div>
		</div>
		<div class="info_link" @click="onClickIcon">{{ store }}</div>
	</div>
</template>

<script lang="ts" setup>
import { onMounted, PropType, ref } from 'vue';
import { getRequireImage } from 'src/utils/imageUtils';
import { CLIENT_TYPE } from 'src/constants/constants';

const props = defineProps({
	type: {
		type: Object as PropType<CLIENT_TYPE>,
		require: true
	},
	value: {
		type: String,
		default: '',
		require: true
	}
});

const imageRef = ref();
const label = ref();
const store = ref();

onMounted(() => {
	switch (props.type) {
		case CLIENT_TYPE.android:
			label.value = 'Android';
			store.value = 'Google Play';
			imageRef.value = getRequireImage('client/android.svg');
			break;
		case CLIENT_TYPE.ios:
			label.value = 'iOS';
			store.value = 'App Store';
			imageRef.value = getRequireImage('client/ios.svg');
			break;
		case CLIENT_TYPE.mac:
			label.value = 'Mac';
			store.value = 'Mac App Store';
			imageRef.value = getRequireImage('client/macos.svg');
			break;
		case CLIENT_TYPE.windows:
			label.value = 'Windows';
			store.value = 'Download';
			imageRef.value = getRequireImage('client/windows.svg');
			break;
		case CLIENT_TYPE.linux:
			label.value = 'Linux';
			store.value = 'Download';
			imageRef.value = getRequireImage('client/linux.svg');
			break;
		case CLIENT_TYPE.edge:
			label.value = 'Edge';
			store.value = 'Edge Addons';
			imageRef.value = getRequireImage('client/edge.svg');
			break;
		case CLIENT_TYPE.chrome:
			label.value = 'Chrome';
			store.value = 'Chrome Web Store';
			imageRef.value = getRequireImage('client/chrome.svg');
			break;
	}
});

const onClickIcon = () => {
	if (props.value) {
		window.open(props.value);
	}
};
</script>

<style scoped lang="scss">
.support-client {
	height: 32px;
	width: 100%;

	.client-icon {
		width: 32px;
		height: 32px;
	}

	.info-content {
		font-family: Roboto;
		font-size: 14px;
		font-weight: 400;
		line-height: 20px;
		letter-spacing: 0em;
		text-align: left;
	}

	.info-label {
		@extend .info-content;
		margin-left: 8px;
		color: var(--Grey-08-, #5c5551);
		text-align: end;
	}

	.info_link {
		@extend .info-content;
		color: #3377ff;
		cursor: pointer;
		text-decoration: none;
	}
}
</style>
