<template>
    <div class="domain-picker">
        <button
            type="button"
            class="ghost domain-option"
            :class="{ active: modelValue === 'pixhost.to' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'pixhost.to')"
        >
            pixhost.to
        </button>
        <button
            type="button"
            class="ghost domain-option"
            :class="{ active: modelValue === 'pixhost.cc' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'pixhost.cc')"
        >
            pixhost.cc
        </button>
    </div>
</template>

<script setup>
defineProps({
    modelValue: { type: String, required: true },
    busy: { type: Boolean, required: true },
});

defineEmits(["update:modelValue"]);
</script>

<style scoped>
.domain-picker {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
}

.domain-option {
    min-width: 108px;
    border-color: rgba(33, 50, 60, 0.16);
    background: rgba(255, 255, 255, 0.88);
    color: var(--muted);
    font-family: var(--font-mono);
    font-weight: 500;
    box-shadow: none;
}

.domain-option.active {
    background: rgba(47, 111, 109, 0.1);
    color: var(--accent);
    border-color: rgba(47, 111, 109, 0.42);
    font-weight: 700;
    box-shadow: 0 1px 2px rgba(14, 22, 30, 0.04);
}

.domain-option:not(.active):hover {
    background: rgba(47, 111, 109, 0.06);
    border-color: rgba(47, 111, 109, 0.28);
}
</style>
