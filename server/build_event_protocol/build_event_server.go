package build_event_server

import (
	"context"
	"io"
	"log"
	"sort"

	"github.com/buildbuddy-io/buildbuddy/server/build_event_protocol/build_event_handler"
	"github.com/buildbuddy-io/buildbuddy/server/environment"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"

	bepb "proto/build_events"
	pepb "proto/publish_build_event"
)

type BuildEventProtocolServer struct {
	env environment.Env
}

func NewBuildEventProtocolServer(env environment.Env) (*BuildEventProtocolServer, error) {
	return &BuildEventProtocolServer{
		env: env,
	}, nil
}

func (s *BuildEventProtocolServer) PublishLifecycleEvent(ctx context.Context, req *pepb.PublishLifecycleEventRequest) (*empty.Empty, error) {
	for _, client := range s.env.GetBuildEventProxyClients() {
		go func() {
			client.PublishLifecycleEvent(ctx, req)
		}()
	}
	// We don't currently handle these events.
	return &empty.Empty{}, nil
}

// Handles Streaming BuildToolEvent
// From the bazel client: (Read more in BuildEventServiceUploader.java.)
// {@link BuildEventServiceUploaderCommands#OPEN_STREAM} is the first event and opens a
// bidi streaming RPC for sending build events and receiving ACKs.
// {@link BuildEventServiceUploaderCommands#SEND_REGULAR_BUILD_EVENT} sends a build event to
// the server. Sending of the Nth build event does
// does not wait for the ACK of the N-1th build event to have been received.
// {@link BuildEventServiceUploaderCommands#SEND_LAST_BUILD_EVENT} sends the last build event
// and half closes the RPC.
// {@link BuildEventServiceUploaderCommands#ACK_RECEIVED} is executed for every ACK from
// the server and checks that the ACKs are in the correct order.
// {@link BuildEventServiceUploaderCommands#STREAM_COMPLETE} checks that all build events
// have been sent and all ACKs have been received. If not it invokes a retry logic that may
// decide to re-send every build event for which an ACK has not been received. If so, it
// adds an OPEN_STREAM event.
func (s *BuildEventProtocolServer) PublishBuildToolEventStream(stream pepb.PublishBuildEvent_PublishBuildToolEventStreamServer) error {
	// Semantically, the protocol requires we ack events in order.
	acks := make([]int, 0)
	var streamID *bepb.StreamId
	var channel *build_event_handler.EventChannel

	forwardingStreams := make([]pepb.PublishBuildEvent_PublishBuildToolEventStreamClient, 0)
	for _, client := range s.env.GetBuildEventProxyClients() {
		stream, err := client.PublishBuildToolEventStream(stream.Context(), grpc.WaitForReady(false))
		if err != nil {
			log.Printf("Unable to proxy stream: %s", err)
			continue
		}
		defer stream.CloseSend()
		forwardingStreams = append(forwardingStreams, stream)
	}

	for {
		in, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if streamID == nil {
			streamID = in.OrderedBuildEvent.StreamId
			channel = build_event_handler.OpenChannel(s.env, stream.Context(), streamID.InvocationId)
		}

		if err := channel.HandleEvent(stream.Context(), in); err != nil {
			log.Printf("Error handling event; this means a broken build command: %s", err)
			return err
		}
		for _, stream := range forwardingStreams {
			// Intentionally ignore errors here -- proxying is best effort.
			stream.Send(in)
		}

		acks = append(acks, int(in.OrderedBuildEvent.SequenceNumber))
	}

	// Check that we have received all acks! If we haven't bail out since we
	// don't want to ack *anything*. This forces the client to retransmit
	// everything all at once, which means we don't need to worry about
	// cross-server consistency of messages in an invocation.
	sort.Sort(sort.IntSlice(acks))
	for i, ack := range acks {
		if ack != i+1 {
			log.Printf("Missing ack: saw %d and wanted %d. Bailing!", ack, i+1)
			return io.EOF
		}
	}

	// Finally, ack everything.
	for _, ack := range acks {
		rsp := &pepb.PublishBuildToolEventStreamResponse{
			StreamId:       streamID,
			SequenceNumber: int64(ack),
		}
		stream.Send(rsp)
	}
	return nil
}
