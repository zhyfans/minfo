<template>
    <section class="panel output">
        <div class="output-header">
            <h2>输出</h2>
            <div class="output-actions">
                <button class="ghost output-copy-btn" @click="$emit('copy')">{{ copyOutputLabel }}</button>
                <button class="ghost" :disabled="busy" @click="$emit('clear')">清空</button>
            </div>
        </div>
        <div class="output-body">
            <div v-if="outputText !== ''" class="output-text">
                <pre>{{ outputText }}</pre>
            </div>
            <div v-if="outputText === ''" class="output-empty">
                {{ busy && statusMessage ? statusMessage : "就绪。" }}
            </div>
        </div>
    </section>
</template>

<script setup>
defineProps({
    busy: { type: Boolean, required: true },
    copyOutputLabel: { type: String, required: true },
    outputText: { type: String, required: true },
    statusMessage: { type: String, required: true },
});

defineEmits(["copy", "clear"]);
</script>
