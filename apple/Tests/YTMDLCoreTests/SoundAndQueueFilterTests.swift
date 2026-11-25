import Testing
@testable import YTMDLCore

private func song(_ id: String, _ title: String, artist: String = "", album: String = "") -> Track {
    Track(id: id, title: title, artists: artist.isEmpty ? [] : [artist], album: album, durationMs: 200_000)
}

@Test func soundStepsSnapToTheGridAndStopAtTheBounds() {
    #expect(SoundStep.adjusted(0, by: 1, in: SoundStep.crossfade) == 1)
    #expect(SoundStep.adjusted(12, by: 1, in: SoundStep.crossfade) == 12)
    #expect(SoundStep.adjusted(0, by: -1, in: SoundStep.crossfade) == 0)
    #expect(!SoundStep.canAdjust(0, by: -1, in: SoundStep.crossfade) && SoundStep.canAdjust(0, by: 1, in: SoundStep.crossfade))
    #expect(!SoundStep.canAdjust(12, by: 1, in: SoundStep.crossfade))
    // A gain set with a finer slider elsewhere moves to the next whole step.
    #expect(SoundStep.adjusted(2.5, by: 1, in: SoundStep.equalizerGain) == 3)
    #expect(SoundStep.adjusted(2.5, by: -1, in: SoundStep.equalizerGain) == 2)
    #expect(SoundStep.adjusted(-11.5, by: -1, in: SoundStep.equalizerGain) == -12)
    #expect(SoundStep.adjusted(5.5, by: 1, in: SoundStep.preamp) == 6)
    #expect(SoundStep.adjusted(20, by: -1, in: SoundStep.preamp) == 5, "An out-of-range value is clamped first.")
    #expect(SoundStep.adjusted(.nan, by: 1, in: SoundStep.equalizerGain) == 1)
    #expect(SoundStep.adjusted(3, by: 0, in: SoundStep.equalizerGain) == 3)
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
