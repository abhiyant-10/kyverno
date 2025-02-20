package leaderelection

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type Interface interface {
	// Run is a blocking call that runs a leader election
	Run(ctx context.Context)

	// ID returns this instances unique identifier
	ID() string

	// Name returns the name of the leader election
	Name() string

	// Namespace is the Kubernetes namespace used to coordinate the leader election
	Namespace() string

	// IsLeader indicates if this instance is the leader
	IsLeader() bool

	// GetLeader returns the leader ID
	GetLeader() string
}

type config struct {
	name              string
	namespace         string
	startWork         func()
	stopWork          func()
	kubeClient        kubernetes.Interface
	lock              resourcelock.Interface
	leaderElectionCfg leaderelection.LeaderElectionConfig
	leaderElector     *leaderelection.LeaderElector
	isLeader          int64
	log               logr.Logger
}

func New(name, namespace string, kubeClient kubernetes.Interface, id string, startWork, stopWork func(), log logr.Logger) (Interface, error) {
	lock, err := resourcelock.New(
		resourcelock.LeasesResourceLock,
		namespace,
		name,
		kubeClient.CoreV1(),
		kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: id,
		},
	)
	if err != nil {
		return nil, errors.Wrapf(err, "error initializing resource lock: %s/%s", namespace, name)
	}
	e := &config{
		name:       name,
		namespace:  namespace,
		kubeClient: kubeClient,
		lock:       lock,
		startWork:  startWork,
		stopWork:   stopWork,
		log:        log.WithValues("id", lock.Identity()),
	}
	e.leaderElectionCfg = leaderelection.LeaderElectionConfig{
		Lock:            e.lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				atomic.StoreInt64(&e.isLeader, 1)
				e.log.Info("started leading")
				if e.startWork != nil {
					e.startWork()
				}
			},
			OnStoppedLeading: func() {
				atomic.StoreInt64(&e.isLeader, 0)
				e.log.Info("leadership lost, stopped leading")
				if e.stopWork != nil {
					e.stopWork()
				}
			},
			OnNewLeader: func(identity string) {
				if identity == e.lock.Identity() {
					e.log.Info("still leading")
				} else {
					e.log.Info("another instance has been elected as leader", "leader", identity)
				}
			},
		},
	}
	e.leaderElector, err = leaderelection.NewLeaderElector(e.leaderElectionCfg)
	if err != nil {
		e.log.Error(err, "failed to create leaderElector")
		os.Exit(1)
	}
	if e.leaderElectionCfg.WatchDog != nil {
		e.leaderElectionCfg.WatchDog.SetLeaderElection(e.leaderElector)
	}
	return e, nil
}

func (e *config) Name() string {
	return e.name
}

func (e *config) Namespace() string {
	return e.namespace
}

func (e *config) ID() string {
	return e.lock.Identity()
}

func (e *config) IsLeader() bool {
	return atomic.LoadInt64(&e.isLeader) == 1
}

func (e *config) GetLeader() string {
	return e.leaderElector.GetLeader()
}

func (e *config) Run(ctx context.Context) {
	e.leaderElector.Run(ctx)
}
