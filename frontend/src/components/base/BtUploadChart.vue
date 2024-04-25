<template>
	<div>
		<input
			ref="uploadInput"
			type="file"
			style="display: none"
			accept=".zip,.tgz"
			@change="uploadFile"
		/>
		<div @click="openFileInput">
			<slot />
		</div>
	</div>
</template>

<script lang="ts" setup>
import { ref } from 'vue';
import { uploadDevFile } from 'src/api/private/operations';
import { useAppStore } from 'src/stores/app';
import { bus, BUS_EVENT } from 'src/utils/bus';

const uploadInput = ref();
const emit = defineEmits(['onSuccess']);
const appStore = useAppStore();

function openFileInput() {
	//clear value before onclick
	uploadInput.value.value = null;
	uploadInput.value.click();
}

async function uploadFile(event: any) {
	console.log(event);
	const file = event.target.files[0];
	if (file) {
		const { name, message } = await uploadDevFile(file);
		if (name) {
			appStore.addDevApplication(name);
			emit('onSuccess');
		} else {
			bus.emit(BUS_EVENT.APP_BACKEND_ERROR, message);
		}
	} else {
		console.log('file selected failure');
	}
}
</script>

<style scoped lang="scss"></style>
