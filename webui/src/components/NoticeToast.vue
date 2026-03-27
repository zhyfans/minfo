<template>
    <transition name="notice-fade">
        <div v-if="text !== ''" class="notice-toast" role="status" aria-live="polite">
            {{ text }}
        </div>
    </transition>
</template>

<script setup>
defineProps({
    text: { type: String, required: true },
});
</script>
