<template>
    <section class="task-progress" :class="{ indeterminate: progress.indeterminate }" aria-live="polite">
        <div class="task-progress-header">
            <strong>{{ progress.stage || "处理中" }}</strong>
            <span>{{ displayPercentLabel }}%</span>
        </div>

        <div class="task-progress-track" role="progressbar" :aria-valuenow="displayPercent" aria-valuemin="0" aria-valuemax="100">
            <div class="task-progress-fill" :style="fillStyle"></div>
        </div>

        <p v-if="detailText !== ''" class="task-progress-detail">{{ detailText }}</p>
    </section>
</template>

<script setup>
import { computed } from "vue";

const props = defineProps({
    progress: { type: Object, required: true },
});

const rawPercent = computed(() => Math.min(100, Math.max(0, Number(props.progress?.percent) || 0)));
const displayPercent = computed(() => Math.round(rawPercent.value));
const displayPercentLabel = computed(() => {
    const value = rawPercent.value;
    const rounded = Math.round(value);
    if (Math.abs(value - rounded) < 0.05) {
        return `${rounded}`;
    }
    return value.toFixed(1);
});

const fillStyle = computed(() => ({
    width: `${rawPercent.value}%`,
}));

const detailText = computed(() => {
    const detail = typeof props.progress?.detail === "string" ? props.progress.detail.trim() : "";
    if (detail !== "") {
        return detail;
    }
    const hasCounter = Number.isFinite(props.progress?.current) && props.progress.current > 0 && Number.isFinite(props.progress?.total) && props.progress.total > 0;
    if (hasCounter) {
        return `${props.progress.current}/${props.progress.total}`;
    }
    return "";
});
</script>
