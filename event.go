//go:build windows
// +build windows

package winlog

import (
	"fmt"
	"syscall"
	"unsafe"
)

type evtCbFunction func(Action uint32, Context uintptr, handle syscall.Handle) uintptr

/*Functionality related to events and listening to the event log*/

// Get a handle to a render context which will render properties from the System element.
//
//	Wraps EvtCreateRenderContext() with Flags = EvtRenderContextSystem. The resulting
//	handle must be closed with CloseEventHandle.
func GetSystemRenderContext() (SysRenderContext, error) {
	context, err := EvtCreateRenderContext(0, 0, EvtRenderContextSystem)
	if err != nil {
		return 0, err
	}
	return SysRenderContext(context), nil
}

/*
	 Get a handle for a event log subscription on the given channel.
	   `query` is an XPath expression to filter the events on the channel - "*" allows all events.
		The resulting handle must be closed with CloseEventHandle.
*/
func CreateListener(channel, query string, startpos EVT_SUBSCRIBE_FLAGS, watcher *LogEventCallbackWrapper) (ListenerHandle, error) {
	wideChan, err := syscall.UTF16PtrFromString(channel)
	if err != nil {
		return 0, err
	}
	wideQuery, err := syscall.UTF16PtrFromString(query)
	if err != nil {
		return 0, err
	}
	listenerHandle, err := EvtSubscribe(0, 0, wideChan, wideQuery, 0, uintptr(0), syscall.NewCallback(newEventCallback(watcher)), uint32(startpos))
	if err != nil {
		return 0, err
	}
	return ListenerHandle(listenerHandle), nil
}

/*
Get a handle for an event log subscription on the given channel. Will begin at the

	bookmarked event, or the closest possible event if the log has been truncated.
	`query` is an XPath expression to filter the events on the channel - "*" allows all events.
	The resulting handle must be closed with CloseEventHandle.
*/
func CreateListenerFromBookmark(channel, query string, watcher *LogEventCallbackWrapper, bookmarkHandle BookmarkHandle) (ListenerHandle, error) {
	wideChan, err := syscall.UTF16PtrFromString(channel)
	if err != nil {
		return 0, err
	}
	wideQuery, err := syscall.UTF16PtrFromString(query)
	if err != nil {
		return 0, err
	}
	listenerHandle, err := EvtSubscribe(0, 0, wideChan, wideQuery, syscall.Handle(bookmarkHandle), uintptr(0), syscall.NewCallback(newEventCallback(watcher)), uint32(EvtSubscribeStartAfterBookmark))
	if err != nil {
		return 0, err
	}
	return ListenerHandle(listenerHandle), nil
}

/* Get the formatted string that represents this message. This method wraps EvtFormatMessage. */
func FormatMessage(eventPublisherHandle PublisherHandle, eventHandle EventHandle, format EVT_FORMAT_MESSAGE_FLAGS) (string, error) {
	var size uint32 = 0
	err := EvtFormatMessage(syscall.Handle(eventPublisherHandle), syscall.Handle(eventHandle), 0, 0, nil, uint32(format), 0, nil, &size)
	if err != nil {
		if errno, ok := err.(syscall.Errno); !ok || errno != 122 {
			// Check if the error is ERR_INSUFICIENT_BUFFER
			return "", err
		}
	}
	buf := make([]uint16, size)
	err = EvtFormatMessage(syscall.Handle(eventPublisherHandle), syscall.Handle(eventHandle), 0, 0, nil, uint32(format), uint32(len(buf)), &buf[0], &size)
	if err != nil {
		return "", err
	}
	return syscall.UTF16ToString(buf), nil
}

/* Get the formatted string for the last error which occurred. Wraps GetLastError and FormatMessage. */
func GetLastError() error {
	return syscall.GetLastError()
}

/*
Render the system properties from the event and returns an array of properties.

	Properties can be accessed using RenderStringField, RenderIntField, RenderFileTimeField,
	or RenderUIntField depending on type. This buffer must be freed after use.
*/
func RenderEventValues(renderContext SysRenderContext, eventHandle EventHandle) (EvtVariant, error) {
	var bufferUsed uint32 = 0
	var propertyCount uint32 = 0
	err := EvtRender(syscall.Handle(renderContext), syscall.Handle(eventHandle), EvtRenderEventValues, 0, nil, &bufferUsed, &propertyCount)
	if bufferUsed == 0 {
		return nil, err
	}
	buffer := make([]byte, bufferUsed)
	bufSize := bufferUsed
	err = EvtRender(syscall.Handle(renderContext), syscall.Handle(eventHandle), EvtRenderEventValues, bufSize, (*uint16)(unsafe.Pointer(&buffer[0])), &bufferUsed, &propertyCount)
	if err != nil {
		return nil, err
	}
	return NewEvtVariant(buffer), nil
}

// Render the event as XML.
func RenderEventXML(eventHandle EventHandle) ([]byte, error) {
	var bufferUsed, propertyCount uint32

	err := EvtRender(0, syscall.Handle(eventHandle), EvtRenderEventXml, 0, nil, &bufferUsed, &propertyCount)

	if bufferUsed == 0 {
		return nil, err
	}

	buffer := make([]uint16, bufferUsed)
	bufSize := bufferUsed

	err = EvtRender(0, syscall.Handle(eventHandle), EvtRenderEventXml, bufSize, &buffer[0], &bufferUsed, &propertyCount)
	if err != nil {
		return nil, err
	}

	return []byte(syscall.UTF16ToString(buffer)), nil
}

/* Get a handle that represents the publisher of the event, given the rendered event values. */
func GetEventPublisherHandle(renderedFields EvtVariant) (PublisherHandle, error) {
	publisher, err := renderedFields.String(EvtSystemProviderName)
	if err != nil {
		return 0, err
	}
	widePublisher, err := syscall.UTF16PtrFromString(publisher)
	if err != nil {
		return 0, err
	}
	handle, err := EvtOpenPublisherMetadata(0, widePublisher, nil, 0, 0)
	if err != nil {
		return 0, err
	}
	return PublisherHandle(handle), nil
}

/* Close an event handle. */
func CloseEventHandle(handle uint64) error {
	return EvtClose(syscall.Handle(handle))
}

/* Cancel pending actions on the event handle. */
func CancelEventHandle(handle uint64) error {
	err := EvtCancel(syscall.Handle(handle))
	if err != nil {
		return err
	}
	return nil
}

/* Get the first event in the log, for testing */
func getTestEventHandle() (EventHandle, error) {
	wideQuery, _ := syscall.UTF16PtrFromString("*")
	wideChannel, _ := syscall.UTF16PtrFromString("Application")
	handle, err := EvtQuery(0, wideChannel, wideQuery, EvtQueryChannelPath)
	if err != nil {
		return 0, err
	}

	var record syscall.Handle
	var recordsReturned uint32
	err = EvtNext(handle, 1, &record, 500, 0, &recordsReturned)
	if err != nil {
		EvtClose(handle)
		return 0, nil
	}
	EvtClose(handle)
	return EventHandle(record), nil
}

// newEventCallback captures the context for use in the callback
func newEventCallback(context *LogEventCallbackWrapper) evtCbFunction {
	return func(action uint32, _ uintptr, handle syscall.Handle) uintptr {
		if action == EvtSubscribeActionError {
			// When the callback is called for an error, the error code is
			// passed in the event handle parameter. See
			// https://learn.microsoft.com/en-us/windows/win32/api/winevt/nc-winevt-evt_subscribe_callback.
			err := syscall.Errno(uintptr(handle))
			context.callback.PublishError(fmt.Errorf("Event log callback got error: %v", err))
		} else {
			context.callback.PublishEvent(EventHandle(handle), context.subscribedChannel)
		}
		return 0
	}
}

// CreateMap converts the WinLogEvent to a map[string]interface{}
func (ev *WinLogEvent) CreateMap() map[string]interface{} {
	toReturn := make(map[string]interface{})
	toReturn["Xml"] = ev.Xml
	toReturn["ProviderName"] = ev.ProviderName
	toReturn["EventId"] = ev.EventId
	toReturn["Qualifiers"] = ev.Qualifiers
	toReturn["Level"] = ev.Level
	toReturn["Task"] = ev.Task
	toReturn["Opcode"] = ev.Opcode
	toReturn["Created"] = ev.Created
	toReturn["RecordId"] = ev.RecordId
	toReturn["ProcessId"] = ev.ProcessId
	toReturn["ThreadId"] = ev.ThreadId
	toReturn["Channel"] = ev.Channel
	toReturn["ComputerName"] = ev.ComputerName
	toReturn["Version"] = ev.Version
	toReturn["Msg"] = ev.Msg
	toReturn["LevelText"] = ev.LevelText
	toReturn["TaskText"] = ev.TaskText
	toReturn["OpcodeText"] = ev.OpcodeText
	toReturn["Keywords"] = ev.Keywords
	toReturn["ChannelText"] = ev.ChannelText
	toReturn["ProviderText"] = ev.ProviderText
	toReturn["IdText"] = ev.IdText
	toReturn["Bookmark"] = ev.Bookmark
	toReturn["SubscribedChannel"] = ev.SubscribedChannel
	toReturn["Bookmark"] = ev.Bookmark
	return toReturn
}
