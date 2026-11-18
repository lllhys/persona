// CGVirtualDisplay: macOS's private virtual display API (macOS 14+), declared for Persona Input's Phone Screen
// (#9314, docs/extensions/computer-use.md "Phone Screen"). The declarations are from Snug
// (https://github.com/zephyr4123/snug, MIT, Copyright (c) 2026 AoyamaShou, Copyright (c) 2026 zephyr4123), which took
// them from FreeDisplay (https://github.com/huberdf/FreeDisplay, MIT); the property names match Chromium's
// virtual_display_mac_util.mm. See THIRD_PARTY_NOTICES.md. initWithDescriptor returns nil while a display with the same
// serial number is still online.

#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>

NS_ASSUME_NONNULL_BEGIN

@interface CGVirtualDisplayDescriptor : NSObject
@property (nonatomic) CGSize sizeInMillimeters;
@property (nonatomic) uint32_t maxPixelsWide;
@property (nonatomic) uint32_t maxPixelsHigh;
@property (nonatomic) NSPoint whitePoint;
@property (nonatomic) NSPoint redPrimary;
@property (nonatomic) NSPoint greenPrimary;
@property (nonatomic) NSPoint bluePrimary;
@property (nonatomic, nullable) dispatch_queue_t queue;
@property (nonatomic, copy) NSString *name;
@property (nonatomic) uint32_t vendorID;
@property (nonatomic) uint32_t productID;
@property (nonatomic) uint32_t serialNum;
@end

@interface CGVirtualDisplayMode : NSObject
- (instancetype)initWithWidth:(NSUInteger)width height:(NSUInteger)height refreshRate:(double)refreshRate;
@property (nonatomic, readonly) NSUInteger width;
@property (nonatomic, readonly) NSUInteger height;
@property (nonatomic, readonly) double refreshRate;
@end

@interface CGVirtualDisplaySettings : NSObject
@property (nonatomic) BOOL hiDPI;
@property (nonatomic, copy) NSArray<CGVirtualDisplayMode *> *modes;
@end

@interface CGVirtualDisplay : NSObject
- (nullable instancetype)initWithDescriptor:(CGVirtualDisplayDescriptor *)descriptor;
- (BOOL)applySettings:(CGVirtualDisplaySettings *)settings;
@property (nonatomic, readonly) CGDirectDisplayID displayID;
@end

NS_ASSUME_NONNULL_END
