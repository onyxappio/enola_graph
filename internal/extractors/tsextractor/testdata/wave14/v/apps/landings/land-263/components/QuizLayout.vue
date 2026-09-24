<script setup lang="ts">
import { useStep } from 'shared-lands-components'

const props = defineProps<{
  title: string
  progressCurrent?: number
  progressTotal?: number
}>()

const { nextDelayed } = useStep()

function handleNextClick() {
  nextDelayed('start')
}
</script>

<template>
  <div class="flex flex-col items-center w-full min-h-[100dvh] bg-[#eef0f8] font-sans pt-12 pb-8">
    <!-- Header -->
    <div class="flex items-center justify-center mb-6">
      <span class="text-3xl font-bold text-[#141b34] tracking-tight">onyx</span>
      <div class="w-2.5 h-2.5 rounded-full bg-[#3ae0ae] ml-0.5 mt-3.5"></div>
    </div>

    <!-- Progress Bar -->
    <div v-if="progressCurrent && progressTotal" class="w-full max-w-[340px] h-2 rounded-full bg-gray-200 mb-10 overflow-hidden">
      <div class="h-full bg-[#4f46e5]" :style="{ width: `${(progressCurrent / progressTotal) * 100}%` }"></div>
    </div>

    <!-- Main Content Area (White Card) -->
    <div class="flex flex-col items-center w-full max-w-[340px] bg-white rounded-[32px] p-6 shadow-xl flex-1 justify-between">
      <h1 class="text-center text-2xl font-bold leading-[1.3] text-[#141b34] mb-8">
        <span v-html="title"></span>
      </h1>

      <slot></slot>

      <button
        class="w-full bg-[#584de3] hover:bg-[#483dcf] text-white font-semibold py-4 rounded-[20px] text-[17px] transition-colors mt-8"
        @click="handleNextClick"
      >
        Continue
      </button>
    </div>
  </div>
</template>

<style scoped>
/* Scoped styles if needed */
</style>
