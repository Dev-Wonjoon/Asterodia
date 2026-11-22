export function createViewerQueue(values, startId, random = Math.random) {
    const ids = [...new Set(values)];
    let shuffled = false, repeat = false, timeline = [...ids];
    let position = Math.max(0, ids.indexOf(startId));
    const mix = list => {
        const result = [...list];
        for (let i = result.length - 1; i > 0; i--) {
            const j = Math.floor(random() * (i + 1));
            [result[i], result[j]] = [result[j], result[i]];
        }
        return result;
    };
    return {
        get current() { return timeline[position]; },
        get count() { return ids.length; },
        get index() { return ids.length ? position % ids.length : 0; },
        get hasPrevious() { return position > 0; },
        get hasNext() { return position + 1 < timeline.length || repeat && ids.length > 0; },
        get shuffled() { return shuffled; },
        get repeat() { return repeat; },
        setRepeat(value) { repeat = Boolean(value); },
        setShuffle(value) {
            if (shuffled === Boolean(value)) return;
            const current = timeline[position];
            shuffled = Boolean(value);
            timeline = shuffled ? [current, ...mix(ids.filter(id => id !== current))] : [...ids];
            position = shuffled ? 0 : Math.max(0, ids.indexOf(current));
        },
        previous() { if (position > 0) position--; return timeline[position]; },
        next() {
            if (position + 1 >= timeline.length && repeat && ids.length) {
                const cycle = shuffled ? mix(ids) : [...ids];
                // A fresh shuffle never immediately repeats the last item, unless it is the only item.
                if (shuffled && cycle.length > 1 && cycle[0] === timeline[position]) [cycle[0], cycle[1]] = [cycle[1], cycle[0]];
                timeline.push(...cycle);
            }
            if (position + 1 < timeline.length) position++;
            return timeline[position];
        },
    };
}
