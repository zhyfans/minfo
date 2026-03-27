export function saveBlob(blob, filename) {
    const url = window.URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    window.URL.revokeObjectURL(url);
}

export async function copyText(text) {
    if (!navigator.clipboard || typeof navigator.clipboard.writeText !== "function") {
        throw new Error("当前环境不支持剪贴板写入。");
    }

    await navigator.clipboard.writeText(text);
}

export function extractDirectLinks(text) {
    if (typeof text !== "string" || text.trim() === "") {
        return [];
    }

    const lines = text.split("\n");
    const links = [];
    const seen = new Set();

    for (const line of lines) {
        const url = normalizeDirectLink(line);
        if (!url || seen.has(url)) {
            continue;
        }
        seen.add(url);
        links.push(url);
    }

    return links;
}

export function normalizeOutputLinks(items) {
    if (!Array.isArray(items)) {
        return [];
    }

    const links = [];
    const seen = new Set();

    for (const item of items) {
        const url = normalizeDirectLink(item?.url);
        if (!url || seen.has(url)) {
            continue;
        }
        seen.add(url);
        links.push({
            id: typeof item?.id === "string" && item.id.trim() !== "" ? item.id : buildLinkId(),
            url,
        });
    }

    return links;
}

export function mergeOutputLinks(existingItems, incomingLinks) {
    const currentItems = normalizeOutputLinks(existingItems);
    const seen = new Set(currentItems.map((item) => item.url));
    const additions = [];
    let duplicateCount = 0;

    for (const link of incomingLinks) {
        const url = normalizeDirectLink(link);
        if (!url) {
            continue;
        }
        if (seen.has(url)) {
            duplicateCount += 1;
            continue;
        }
        seen.add(url);
        additions.push({ id: buildLinkId(), url });
    }

    return {
        items: [...currentItems, ...additions],
        addedCount: additions.length,
        duplicateCount,
    };
}

export function buildCopyText(outputText, linkItems) {
    const text = typeof outputText === "string" ? outputText.trim() : "";
    const links = normalizeOutputLinks(linkItems).map((item) => item.url);
    const parts = [];

    if (text !== "") {
        parts.push(text);
    }
    if (links.length > 0) {
        parts.push(links.join("\n"));
    }

    return parts.join("\n\n").trim();
}

export function buildLinkText(linkItems) {
    return normalizeOutputLinks(linkItems)
        .map((item) => item.url)
        .join("\n")
        .trim();
}

export function buildBBCodeText(linkItems) {
    return normalizeOutputLinks(linkItems)
        .map((item) => `[img]${item.url}[/img]`)
        .join("\n")
        .trim();
}

function normalizeDirectLink(value) {
    if (typeof value !== "string") {
        return "";
    }

    const url = value.trim();
    if (url === "") {
        return "";
    }
    if (!url.startsWith("http://") && !url.startsWith("https://")) {
        return "";
    }
    if (/[\s[\]()<>"]/.test(url)) {
        return "";
    }

    return url;
}

function buildLinkId() {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return crypto.randomUUID();
    }
    return `shot-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
