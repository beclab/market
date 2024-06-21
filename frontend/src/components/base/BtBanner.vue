<template>
	<div class="banner-background justify-start items-center">
		<q-carousel
			class="banner-carousel"
			animated
			v-model="slide"
			:autoplay="10000"
			swipeable
			infinite
			keep-alive
			transition-prev="slide-right"
			transition-next="slide-left"
		>
			<template v-for="item in size" :key="item">
				<q-carousel-slide style="padding: 0" :name="item">
					<slot name="slide" :index="item" />
				</q-carousel-slide>
			</template>
		</q-carousel>

		<div
			v-if="size > 1"
			class="cursor-pointer banner-navigation row justify-start items-center"
		>
			<template v-for="item in size" :key="item">
				<div
					:class="item === slide ? 'navigation-active' : 'navigation-normal'"
					@click="changeActive(item)"
				/>
			</template>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { ref } from 'vue';

const props = defineProps({
	size: {
		type: Number,
		require: true
	},
	start: {
		type: Number,
		default: 1
	}
});

const slide = ref(props.start);

const changeActive = (index: number) => {
	slide.value = index;
};
</script>

<style scoped lang="scss">
.banner-background {
	width: 100%;
	height: 100%;
	position: relative;

	.banner-carousel {
		width: 100%;
		height: 100%;
	}

	.banner-navigation {
		width: auto;
		height: 20px;
		right: 64px;
		bottom: 32px;
		position: absolute;

		.navigation-base {
			height: 8px;
			border-radius: 20px;
			margin-right: 8px;
		}

		.navigation-active {
			@extend .navigation-base;
			width: 32px;
			background-color: #ffffff;
		}

		.navigation-normal {
			@extend .navigation-base;
			width: 8px;
			background: rgba(255, 255, 255, 0.4);
		}
	}
}
</style>
