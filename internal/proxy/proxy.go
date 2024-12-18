package proxy

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"time"

	"hyperspike.io/pivot/internal/spool"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type Forwarder struct {
	ctx          context.Context
	Client       *restclient.RESTClient
	Config       *restclient.Config
	Ports        []string
	StopChannel  chan struct{}
	ReadyChannel chan struct{}
	*genericiooptions.IOStreams
}

func (f *Forwarder) createDialer(url *url.URL) (httpstream.Dialer, error) {
	transport, upgrader, err := spdy.RoundTripperFor(f.Config)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)
	/*
		if !cmdutil.PortForwardWebsockets.IsDisabled() {
			tunnelingDialer, err := portforward.NewSPDYOverWebsocketDialer(url, f.Config)
			if err != nil {
				return nil, err
			}
			// First attempt tunneling (websocket) dialer, then fallback to spdy dialer.
			dialer = portforward.NewFallbackDialer(tunnelingDialer, dialer, func(err error) bool {
				return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
			})
		}
	*/
	return dialer, nil
}

func NewForwarder(ctx context.Context, kubeContext string) (*Forwarder, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	f := &Forwarder{
		ctx:          ctx,
		IOStreams:    &genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
		StopChannel:  make(chan struct{}, 1),
		ReadyChannel: make(chan struct{}),
	}
	spool.KubeContext = kubeContext
	rest, err := spool.GetKubeConfig()
	if err != nil {
		return nil, err
	}
	rest.GroupVersion = &schema.GroupVersion{Group: "api", Version: "v1"}
	rest.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	f.Config = rest
	f.Client, err = restclient.RESTClientFor(f.Config)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Forwarder) ForwardPorts(name, namespace, port string) error {
	if name == "" {
		name = "gitea-0"
	}
	if namespace == "" {
		namespace = "default"
	}
	if port == "" {
		port = "3000"
	}
	// GET /api/v1/namespaces/default/pods/gitea-0
	req := f.Client.Get().
		Resource("pods").
		Namespace(namespace).
		Name(name)
	tries := 0
	for {
		resp := req.Do(f.ctx)
		if resp.Error() != nil {
			tries = tries + 1
			time.Sleep(5 * time.Second)
			if tries > 60 {
				return resp.Error()
			}
			continue
		}
		pod := &corev1.Pod{}
		if err := resp.Into(pod); err != nil {
			return err
		}
		if pod.Status.Phase == corev1.PodRunning {
			break
		}
		tries = tries + 1
		time.Sleep(5 * time.Second)
		if tries > 60 {
			return resp.Error()
		}
	}
	// POST /api/v1/namespaces/default/pods/gitea-0/portforward
	req = f.Client.Post().
		Resource("pods").
		Namespace(namespace).
		Name(name).
		SubResource("portforward")
	dialer, err := f.createDialer(req.URL())
	if err != nil {
		return err
	}
	fw, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{port}, f.StopChannel, f.ReadyChannel, f.Out, f.ErrOut)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}
