<template>
    <div class="actions">
        <button class="action-btn" :class="{ loading: activeAction === 'mediainfo' }" :disabled="busy || !hasInput" @click="$emit('mediainfo')">
            <span v-if="activeAction === 'mediainfo'" class="action-btn-spinner"></span>
            <span>{{ activeAction === "mediainfo" ? "生成中..." : "生成 MediaInfo" }}</span>
        </button>
        <button class="action-btn" :class="{ loading: activeAction === 'bdinfo' }" :disabled="busy || !hasInput" @click="$emit('bdinfo')">
            <span v-if="activeAction === 'bdinfo'" class="action-btn-spinner"></span>
            <span>{{ activeAction === "bdinfo" ? "生成中..." : "生成 BDInfo" }}</span>
        </button>
        <button class="action-btn" :class="{ loading: activeAction === 'download-shots' }" :disabled="busy || !hasInput" @click="$emit('download-shots')">
            <span v-if="activeAction === 'download-shots'" class="action-btn-spinner"></span>
            <span>{{ activeAction === "download-shots" ? "下载中..." : "下载 4 张截图" }}</span>
        </button>
        <button class="action-btn" :class="{ loading: activeAction === 'output-links' }" :disabled="busy || !hasInput" @click="$emit('output-links')">
            <span v-if="activeAction === 'output-links'" class="action-btn-spinner"></span>
            <span>{{ activeAction === "output-links" ? "生成中..." : "生成图床链接" }}</span>
        </button>
    </div>
</template>

<script setup>
defineProps({
    busy: { type: Boolean, required: true },
    activeAction: { type: String, required: true },
    hasInput: { type: Boolean, required: true },
});

defineEmits(["mediainfo", "bdinfo", "download-shots", "output-links"]);
</script>
