<template>
    <div class="grain"></div>
    <main class="shell">
        <header class="hero">
            <div>
                <p class="kicker">本地媒体检测</p>
                <h1>minfo</h1>
                <p class="lead">一键生成 MediaInfo / BDInfo，一键截图。</p>
            </div>
        </header>

        <section class="panel">
            <div class="field">
                <label for="path-search">媒体选择</label>
                <div class="path-picker">
                    <div class="browser integrated">
                        <div class="browser-toolbar">
                            <div class="browser-current">{{ browserDir || "可用挂载路径" }}</div>
                        </div>

                        <div class="browser-search">
                            <div class="search-actions">
                                <button
                                    class="ghost icon-btn"
                                    :disabled="busy || browserLoading || !canNavigateUp"
                                    title="上一级"
                                    aria-label="上一级"
                                    @click="navigateUp"
                                >
                                    ⬆
                                </button>
                                <button
                                    class="ghost icon-btn"
                                    :disabled="busy || browserLoading"
                                    title="刷新"
                                    aria-label="刷新"
                                    @click="refreshBrowser"
                                >
                                    ↻
                                </button>
                            </div>
                            <input
                                id="path-search"
                                type="text"
                                v-model="searchKeyword"
                                placeholder="模糊搜索当前目录"
                            />
                        </div>

                        <div class="browser-error" v-if="browserError !== ''">
                            {{ browserError }}
                        </div>

                        <div class="browser-list">
                            <div class="browser-row empty" v-if="browserLoading">
                                加载中...
                            </div>
                            <div class="browser-row empty" v-else-if="filteredEntries.length === 0">
                                当前目录无匹配项
                            </div>
                            <div
                                class="browser-row"
                                :class="{
                                    selected: normalizeComparePath(path) === normalizeComparePath(entry.path),
                                    directory: entry.isDir,
                                    locked: busy || browserLoading,
                                }"
                                v-for="entry in filteredEntries"
                                :key="entry.path"
                                @click="choosePath(entry.path)"
                                @dblclick="handleEntryDoubleClick(entry)"
                            >
                                <span class="browser-row-name">{{ entry.isDir ? `📁 ${entry.name}` : `📄 ${entry.name}` }}</span>
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            <div class="actions">
                <button :disabled="busy || !hasInput()" @click="runInfo('/api/mediainfo', 'MediaInfo')">生成 MediaInfo</button>
                <button :disabled="busy || !hasInput()" @click="runInfo('/api/bdinfo', 'BDInfo')">生成 BDInfo</button>
                <button :disabled="busy || !hasInput()" @click="downloadShots">下载 4 张截图</button>
            </div>
        </section>

        <section class="panel output">
            <div class="output-header">
                <h2>输出</h2>
                <div class="output-actions">
                    <button class="ghost" @click="copyOutput">{{ copyLabel }}</button>
                    <button class="ghost" :disabled="busy" @click="clearOutput">清空</button>
                </div>
            </div>
            <pre>{{ output }}</pre>
        </section>
    </main>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";

const path = ref("");
const output = ref("就绪。");
const busy = ref(false);
const copyLabel = ref("复制");

const browserRoots = ref([]);
const browserRoot = ref("");
const browserDir = ref("");
const browserEntries = ref([]);
const browserLoading = ref(false);
const browserError = ref("");
const searchKeyword = ref("");
let browserController = null;

const hasInput = () => path.value.trim() !== "";

const normalizeComparePath = (value) => {
    if (!value) {
        return "";
    }
    if (value === "/" || value === "\\") {
        return "/";
    }
    return value.replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
};

const canNavigateUp = computed(() => {
    if (!browserDir.value) {
        return false;
    }
    const root = normalizeComparePath(browserRoot.value);
    const current = normalizeComparePath(browserDir.value);
    if (root === "") {
        return true;
    }
    if (current !== root) {
        return true;
    }
    return browserRoots.value.length > 1;
});

const filteredEntries = computed(() => {
    const keyword = searchKeyword.value.trim().toLowerCase();
    if (keyword === "") {
        return browserEntries.value;
    }
    return browserEntries.value.filter((entry) => {
        const name = (entry.name || "").toLowerCase();
        const full = (entry.path || "").toLowerCase();
        return name.includes(keyword) || full.includes(keyword);
    });
});

const withTrailingSeparator = (value) => {
    if (value === "") {
        return "";
    }
    if (value.endsWith("/") || value.endsWith("\\")) {
        return value;
    }
    const separator = value.includes("\\") && !value.includes("/") ? "\\" : "/";
    return `${value}${separator}`;
};

const cleanPath = (value) => {
    if (!value) {
        return "";
    }
    if (value === "/" || value === "\\") {
        return value;
    }
    return value.replace(/[\\/]+$/, "");
};

const getEntryName = (value) => {
    const normalized = value.replace(/[\\/]+$/, "");
    if (normalized === "") {
        return value;
    }
    const parts = normalized.split(/[\\/]/);
    return parts[parts.length - 1] || normalized;
};

const buildEntries = (items) => {
    const result = [];
    for (const raw of items) {
        if (typeof raw !== "string" || raw.trim() === "") {
            continue;
        }
        const isDir = raw.endsWith("/") || raw.endsWith("\\");
        const clean = cleanPath(raw);
        result.push({
            path: clean,
            name: getEntryName(raw),
            isDir,
        });
    }
    result.sort((a, b) => {
        if (a.isDir !== b.isDir) {
            return a.isDir ? -1 : 1;
        }
        return a.name.localeCompare(b.name, "zh-CN");
    });
    return result;
};

const setBusy = (isBusy, label) => {
    busy.value = isBusy;
    if (label) {
        output.value = label;
    }
};

const appendOutput = (text) => {
    output.value = text;
};

const errorOutput = (message) => {
    output.value = `错误：${message}`;
};

const fetchDirectory = async (prefix) => {
    if (browserController) {
        browserController.abort();
    }
    browserController = new AbortController();

    const url = new URL("/api/path", window.location.origin);
    if (prefix !== "") {
        url.searchParams.set("prefix", prefix);
    }

    const res = await fetch(url.toString(), { signal: browserController.signal });
    const data = await res.json();
    if (!res.ok || !data.ok || !Array.isArray(data.items)) {
        throw new Error(data.error || "读取路径失败。");
    }
    return data;
};

const loadDirectory = async (dir) => {
    browserLoading.value = true;
    browserError.value = "";
    try {
        const prefix = dir ? withTrailingSeparator(dir) : "";
        const data = await fetchDirectory(prefix);
        browserRoots.value = Array.isArray(data.roots) ? data.roots.map(cleanPath).filter(Boolean) : [];
        if (typeof data.root === "string") {
            browserRoot.value = cleanPath(data.root);
        }

        browserEntries.value = buildEntries(data.items);

        if (dir && dir !== "") {
            browserDir.value = cleanPath(dir);
        } else if (browserRoot.value !== "") {
            browserDir.value = browserRoot.value;
        } else {
            browserDir.value = "";
        }

        searchKeyword.value = "";
    } catch (err) {
        if (err && err.name === "AbortError") {
            return;
        }
        browserError.value = err && err.message ? err.message : "读取路径失败。";
        browserEntries.value = [];
    } finally {
        browserLoading.value = false;
    }
};

const parentDirectory = (dir) => {
    const normalized = cleanPath(dir);
    if (normalized === "" || normalized === "/") {
        return normalized;
    }
    const slash = Math.max(normalized.lastIndexOf("/"), normalized.lastIndexOf("\\"));
    if (slash <= 0) {
        return browserRoot.value || "";
    }
    return normalized.slice(0, slash);
};

const navigateUp = async () => {
    if (!browserDir.value) {
        await loadDirectory("");
        return;
    }

    let parent = parentDirectory(browserDir.value);
    const root = normalizeComparePath(browserRoot.value);
    const current = normalizeComparePath(browserDir.value);
    if (root !== "" && current === root && browserRoots.value.length > 1) {
        await loadDirectory("");
        return;
    }
    if (root !== "" && normalizeComparePath(parent).length < root.length) {
        parent = browserRoot.value;
    }

    if (parent === browserDir.value && browserRoot.value && browserDir.value !== browserRoot.value) {
        parent = browserRoot.value;
    }

    await loadDirectory(parent || "");
};

const refreshBrowser = async () => {
    await loadDirectory(browserDir.value || "");
};

const choosePath = (value) => {
    if (busy.value || browserLoading.value) {
        return;
    }
    path.value = value;
};

const handleEntryDoubleClick = async (entry) => {
    if (busy.value || browserLoading.value) {
        return;
    }
    if (entry.isDir) {
        await loadDirectory(entry.path);
        return;
    }
    choosePath(entry.path);
};

const postForm = async (url) => {
    const form = new FormData();
    const value = path.value.trim();
    if (value !== "") {
        form.append("path", value);
    }
    return fetch(url, { method: "POST", body: form });
};

const runInfo = async (url, label) => {
    if (!hasInput()) {
        errorOutput("请先选择媒体路径。");
        return;
    }
    try {
        setBusy(true, `${label} 生成中...`);
        const res = await postForm(url);
        let data = {};
        try {
            data = await res.json();
        } catch (err) {
            data = {};
        }
        if (!res.ok || !data.ok) {
            throw new Error(data.error || "请求失败。");
        }
        appendOutput(data.output || "没有输出。");
    } catch (err) {
        errorOutput(err && err.message ? err.message : "请求失败。");
    } finally {
        setBusy(false);
    }
};

const downloadShots = async () => {
    if (!hasInput()) {
        errorOutput("请先选择媒体路径。");
        return;
    }
    try {
        setBusy(true, "正在生成截图...");
        const res = await postForm("/api/screenshots");
        const contentType = res.headers.get("content-type") || "";
        if (!res.ok || !contentType.includes("application/zip")) {
            let data = {};
            try {
                data = await res.json();
            } catch (err) {
                data = {};
            }
            throw new Error(data.error || "截图请求失败。");
        }
        const blob = await res.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = "screenshots.zip";
        document.body.appendChild(a);
        a.click();
        a.remove();
        window.URL.revokeObjectURL(url);
        appendOutput("截图已下载为 screenshots.zip。");
    } catch (err) {
        errorOutput(err && err.message ? err.message : "截图请求失败。");
    } finally {
        setBusy(false);
    }
};

const clearOutput = () => {
    if (busy.value) {
        return;
    }
    appendOutput("就绪。");
};

const copyOutput = async () => {
    const text = output.value || "";
    if (text.trim() === "") {
        errorOutput("没有可复制的内容。");
        return;
    }

    try {
        await navigator.clipboard.writeText(text);
    } catch (err) {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "absolute";
        textarea.style.left = "-9999px";
        document.body.appendChild(textarea);
        textarea.select();
        try {
            document.execCommand("copy");
        } finally {
            textarea.remove();
        }
    }

    const original = copyLabel.value;
    copyLabel.value = "已复制";
    setTimeout(() => {
        copyLabel.value = original;
    }, 1200);
};

onMounted(async () => {
    await loadDirectory("");
});

onBeforeUnmount(() => {
    if (browserController) {
        browserController.abort();
    }
});
</script>
