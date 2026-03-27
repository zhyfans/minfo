<template>
    <div class="actions">
        <button :disabled="busy || !hasInput" @click="$emit('mediainfo')">生成 MediaInfo</button>
        <button :disabled="busy || !hasInput" @click="$emit('bdinfo')">生成 BDInfo</button>
        <button :disabled="busy || !hasInput" @click="$emit('download-shots')">下载 4 张截图</button>
        <button :disabled="busy || !hasInput" @click="$emit('output-links')">生成图床链接</button>
    </div>
</template>

<script setup>
defineProps({
    busy: { type: Boolean, required: true },
    hasInput: { type: Boolean, required: true },
});

defineEmits(["mediainfo", "bdinfo", "download-shots", "output-links"]);
</script>
