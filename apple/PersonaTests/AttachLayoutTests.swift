import CoreGraphics
import Testing
@testable import Persona

struct AttachLayoutTests {
    @Test func theTabBar() {
        #expect(AttachLayout.barWidth(tabs: 2, width: 402) == 160)
        #expect(AttachLayout.barWidth(tabs: 3, width: 402) == 240)
        #expect(AttachLayout.barWidth(tabs: 6, width: 402) == 358)
        #expect(AttachLayout.barWidth(tabs: 3, width: 200) == 156)
    }

    /// Three equal square columns that fit the width, on the pixel (#4312): the camera's cell is one of them.
    @Test func theGridsCellsAreEqualAndFit() {
        #expect(AttachLayout.cell(width: 402) == 400.0 / 3)
        for width in [320.0, 375, 390, 393, 402, 430, 440] as [CGFloat] {
            let cell = AttachLayout.cell(width: width)
            let row = cell * CGFloat(Theme.Attach.columns) + Theme.Attach.gridSpacing * CGFloat(Theme.Attach.columns - 1)
            #expect(row <= width, "a row is wider than \(width)")
            #expect(width - row < 1, "a row leaves a gap at \(width)")
            #expect((cell * 3).rounded() == cell * 3, "the cell is off the pixel at \(width)")
            let twice = AttachLayout.cell(width: width, scale: 2)
            #expect((twice * 2).rounded() == twice * 2, "the cell is off a 2x pixel at \(width)")
        }
    }
}
