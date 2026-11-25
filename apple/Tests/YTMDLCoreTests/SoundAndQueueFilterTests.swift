import Testing
@testable import YTMDLCore

private func song(_ id: String, _ title: String, artist: String = "", album: String = "") -> Track {
    Track(id: id, title: title, artists: artist.isEmpty ? [] : [artist], album: album, durationMs: 200_000)
}

@Test func soundPickerOptionsCoverTheRangeAndKeepTheCurrentValue() {
    let crossfade = SoundStep.options(around: 0, step: 1, in: SoundStep.crossfade)
    #expect(crossfade.count == 13 && crossfade.first == 0 && crossfade.last == 12)
    let gains = SoundStep.options(around: 3, step: 1, in: SoundStep.equalizerGain)
    #expect(gains.count == 25 && gains.first == -12 && gains.last == 12)
    #expect(SoundStep.options(around: 0, step: 1, in: SoundStep.preamp).count == 19)
    // A gain set with a finer slider elsewhere stays selectable, in order.
    let fine = SoundStep.options(around: 2.5, step: 1, in: SoundStep.equalizerGain)
    #expect(fine.count == 26 && fine.contains(2.5) && fine == fine.sorted())
    #expect(SoundStep.options(around: 20, step: 1, in: SoundStep.preamp).last == 6, "An out-of-range value is clamped.")
    #expect(SoundStep.options(around: .nan, step: 1, in: SoundStep.crossfade).count == 13)
    #expect(SoundStep.options(around: 3, step: 0, in: SoundStep.crossfade) == [3])
    #expect(SoundStep.playbackRates.contains(1) && SoundStep.playbackRates.first == 0.5 && SoundStep.playbackRates.last == 2)
}

@Test func queueFilterMatchesTitleArtistAndAlbumAndKeepsPositions() {
    var queue = PlaybackQueue()
    queue.replace([song("a", "Nachtfahrt", artist: "Nova"), song("b", "Zeitlos", album: "Blau"), song("c", "Fernweh"), song("d", "Blaue Stunde")])
    #expect(queue.rows(filter: "").map(\.position) == [0, 1, 2, 3])
    #expect(queue.rows(filter: "   ").count == 4, "A blank filter shows everything.")
    #expect(queue.rows(filter: "blau").map(\.position) == [1, 3], "Album and title match, ignoring case.")
    #expect(queue.rows(filter: "NOVA").map(\.position) == [0])
    #expect(queue.rows(filter: "xyz").isEmpty)
    // Editing a filtered row edits that entry of the whole queue.
    let rows = queue.rows(filter: "Blaue")
    #expect(rows.count == 1 && rows[0].position == 3 && rows[0].startsSection)
    let moved = queue.playNext(at: rows[0].position)
    #expect(moved)
    #expect(queue.tracks.map(\.id) == ["a", "d", "b", "c"])
}

@Test func focusAfterRemovingAFilteredRowStaysOnAVisibleRow() {
    // Visible before removing 3: 1, 3, 6. The next visible row moves up by one.
    #expect(PlaybackQueue.focusAfterRemoving(3, visible: [1, 3, 6]) == 5)
    #expect(PlaybackQueue.focusAfterRemoving(6, visible: [1, 3, 6]) == 3, "After the last visible row, the one before it.")
    #expect(PlaybackQueue.focusAfterRemoving(1, visible: [1]) == nil)
    #expect(PlaybackQueue.focusAfterRemoving(2, visible: [0, 1, 2, 3]) == 2, "Unfiltered it matches focusAfterRemoving(_:).")
}
