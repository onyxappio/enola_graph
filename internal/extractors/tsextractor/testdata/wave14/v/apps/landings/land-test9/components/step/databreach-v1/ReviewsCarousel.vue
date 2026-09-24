<script setup lang="ts">
if (import.meta.dev) {
  setLandPageMetadata({ page: 'step/databreach-v1/ReviewsCarousel', description: 'Rotating social proof quotes' })
}

const props = withDefaults(defineProps<{
  /** Parent-driven index (e.g. synced to analyze loader clock) */
  activeIndex?: number
  /** Ms each review stays visible – only used when activeIndex is omitted */
  slideMs?: number
  loop?: boolean
}>(), {
  slideMs: 5000,
  loop: true,
})

const reviews = [
  { quote: 'Found a 2017 password I\'d forgotten about. Changed it before someone tried it on my bank account.', name: 'Sarah M.', loc: 'Ohio' },
  { quote: 'Saw my address listed on 14 sites I\'d never heard of. The scan gave me a clear list to act on.', name: 'Michael T.', loc: 'Texas' },
  { quote: 'My phone number was on more scam-call lists than I imagined. Stopped the daily robocalls within a week.', name: 'Patricia L.', loc: 'New York' },
]

const reviewIndex = ref(0)

const isControlled = computed(() => props.activeIndex !== undefined)

watch(
  () => props.activeIndex,
  (idx) => {
    if (idx === undefined) return
    reviewIndex.value = Math.min(Math.max(idx, 0), reviews.length - 1)
  },
  { immediate: true },
)

const { pause, resume } = useIntervalFn(() => {
  if (isControlled.value) return
  if (reviewIndex.value < reviews.length - 1) {
    reviewIndex.value += 1
    return
  }
  if (props.loop) {
    reviewIndex.value = 0
  } else {
    pause()
  }
}, () => props.slideMs, { immediate: false })

watch(isControlled, (controlled) => {
  if (controlled) pause()
  else resume()
}, { immediate: true })

onBeforeUnmount(() => pause())

const currentReview = computed(() => reviews[reviewIndex.value])
</script>

<template>
  <div class="flex w-full flex-col gap-4 rounded-[20px] border border-white/5 bg-[var(--color-bg-surface-tertiary)] p-5">
    <div class="flex items-center justify-between gap-3">
      <p class="section-label">What people say</p>
      <p class="flex items-center gap-1 text-[14px] font-semibold text-white">
        <span>4.8</span>
        <StepDatabreachV1RatingStars :count="1" :size="16" :gap="0" />
      </p>
    </div>
    <transition name="review" mode="out-in">
      <div :key="currentReview.name">
        <StepDatabreachV1RatingStars :size="16" :gap="2" />
        <p class="mt-2.5 m-0 text-[16px] font-medium leading-[22px] tracking-[-.16px] text-white">"{{ currentReview.quote }}"</p>
        <p class="mt-3 m-0 text-[11.5px] font-semibold uppercase tracking-[.12em] text-[var(--color-text-subdued)]"> {{ currentReview.name }}, {{ currentReview.loc }}</p>
      </div>
    </transition>
    <div class="review-dots">
      <button
        v-for="(_, i) in reviews"
        :key="i"
        type="button"
        class="review-dot"
        :class="i === reviewIndex ? 'review-dot--active' : 'review-dot--inactive'"
        :aria-label="`Show review ${i + 1}`"
        :aria-current="i === reviewIndex ? 'true' : undefined"
        @click="reviewIndex = i"
      />
    </div>
  </div>
</template>

<style scoped>
.section-label {
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: .18em;
  text-transform: uppercase;
  color: var(--alpha-white-45);
  margin: 0;
}

.review-dots {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}

.review-dot {
  border: none;
  padding: 0;
  margin: 0;
  box-shadow: none;
  outline: none;
  appearance: none;
  -webkit-appearance: none;
  cursor: pointer;
  flex-shrink: 0;
  transition: width 200ms ease, background-color 200ms ease;
}

.review-dot--active {
  width: 16px;
  height: 6px;
  border-radius: 999px;
  background: var(--color-brand);
}

.review-dot--inactive {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--alpha-white-16);
}

.review-enter-active, .review-leave-active {
  transition: opacity 320ms ease, transform 320ms cubic-bezier(.16, 1, .3, 1);
}
.review-enter-from { opacity: 0; transform: translateY(8px); }
.review-leave-to { opacity: 0; transform: translateY(-8px); }
</style>
