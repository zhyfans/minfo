<template>
    <div class="grain"></div>
    <main class="shell">
        <AppHeader />

        <section class="panel">
            <PathBrowser
                v-model:path="path"
                v-model:search-keyword="searchKeyword"
                :busy="busy"
                :browser-dir="browserDir"
                :browser-error="browserError"
                :browser-loading="browserLoading"
                :can-navigate-up="canNavigateUp"
                :entries="filteredEntries"
                @navigate-up="navigateUp"
                @refresh="refreshBrowser"
                @open-entry="handleEntryDoubleClick"
            />

            <div class="field">
                <label>截图模式</label>
                <ScreenshotVariantPicker v-model="screenshotVariant" :busy="busy" />
            </div>

            <ActionButtons
                :busy="busy"
                :has-input="hasInput"
                @mediainfo="runInfo('/api/mediainfo', 'MediaInfo')"
                @bdinfo="runInfo('/api/bdinfo', 'BDInfo')"
                @download-shots="downloadShots"
                @output-links="outputShotLinks"
            />
        </section>

        <OutputPanel :busy="busy" :copy-output-label="copyOutputLabel" :output-text="outputText" :status-message="statusMessage" @copy="copyOutputText" @clear="clearOutputText" />

        <ImageLinksPanel
            :busy="busy"
            :copy-links-label="copyLinksLabel"
            :copy-b-b-code-label="copyBBCodeLabel"
            :link-status-text="linkStatusText"
            :link-items="linkItems"
            @append-links="appendShotLinks"
            @copy-links="copyLinks"
            @copy-bbcode="copyBBCode"
            @clear="clearLinkItems"
            @remove-link="removeLink"
        />
    </main>
</template>

<script setup>
import { ref, watch } from "vue";
import ActionButtons from "./components/ActionButtons.vue";
import AppHeader from "./components/AppHeader.vue";
import ImageLinksPanel from "./components/ImageLinksPanel.vue";
import OutputPanel from "./components/OutputPanel.vue";
import PathBrowser from "./components/PathBrowser.vue";
import ScreenshotVariantPicker from "./components/ScreenshotVariantPicker.vue";
import { useMediaActions } from "./composables/useMediaActions";
import { usePathBrowser } from "./composables/usePathBrowser";
import { loadAppState, saveAppState } from "./utils/storage";

const persistedState = loadAppState();
const screenshotVariant = ref(persistedState.screenshotVariant);
const pathBrowser = usePathBrowser({
    initialPath: persistedState.path,
    initialBrowserDir: persistedState.browserDir,
});
const mediaActions = useMediaActions(pathBrowser.path, screenshotVariant, pathBrowser.hasInput, {
    initialOutputText: persistedState.outputText,
    initialLinkItems: persistedState.linkItems,
});

const {
    path,
    searchKeyword,
    browserDir,
    browserError,
    browserLoading,
    canNavigateUp,
    filteredEntries,
    hasInput,
    navigateUp,
    refreshBrowser,
    handleEntryDoubleClick,
} = pathBrowser;

const {
    outputText,
    linkItems,
    busy,
    linkStatusText,
    copyOutputLabel,
    copyLinksLabel,
    copyBBCodeLabel,
    statusMessage,
    runInfo,
    downloadShots,
    outputShotLinks,
    appendShotLinks,
    clearOutputText,
    clearLinkItems,
    copyOutputText,
    copyLinks,
    copyBBCode,
    removeLink,
} = mediaActions;

watch(
    [path, browserDir, screenshotVariant, outputText, linkItems],
    ([nextPath, nextBrowserDir, nextVariant, nextOutputText, nextLinkItems]) => {
        saveAppState({
            path: nextPath,
            browserDir: nextBrowserDir,
            screenshotVariant: nextVariant,
            outputText: nextOutputText,
            linkItems: nextLinkItems,
        });
    },
    { deep: true, immediate: true },
);
</script>
